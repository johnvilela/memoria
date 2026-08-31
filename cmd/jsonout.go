package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// parseProcessorJSON is the single trust boundary for every processor reply:
// extract the object, unmarshal it into dst, and on failure run one
// deterministic repair pass before giving up. Whole markdown bodies ride
// inside JSON strings, so the model has to escape every quote in them — it
// eventually won't, and one dropped backslash used to kill a whole batch
// ("invalid character 'f' after object key:value pair"). Repairing in Go beats
// a second LLM round-trip: no tokens, no latency, same answer every time.
//
// Unparseable output is dumped next to the project so the next failure is
// diagnosable instead of anonymous. Returns repaired=true when the repair pass
// is what made it parse — callers surface that rather than hiding it.
func parseProcessorJSON(raw, projectDir, component string, out io.Writer, dst any) (bool, error) {
	s, err := extractJSON(raw)
	if err != nil {
		return false, fmt.Errorf("%w%s", err, dumpNote(projectDir, component, raw, "", err))
	}
	perr := json.Unmarshal([]byte(s), dst)
	if perr == nil {
		return false, nil
	}
	if fixed := repairJSON(s); fixed != s {
		if json.Unmarshal([]byte(fixed), dst) == nil {
			// keep the raw around: repair can in principle mangle a body, and
			// the only way to check is to read what the model actually sent
			p := dumpProcessorOutput(projectDir, component, raw, s, fmt.Errorf("repaired: %w", perr))
			fmt.Fprintf(out, "warning: processor output was malformed JSON (%v) — repaired deterministically", perr)
			if p != "" {
				fmt.Fprintf(out, "; raw kept at %s", p)
			}
			fmt.Fprintln(out)
			logf(component, "repaired malformed JSON from processor: %v", perr)
			return true, nil
		}
		perr = fmt.Errorf("%w (repair pass did not help)", perr)
	}
	return false, fmt.Errorf("processor returned invalid JSON: %w%s",
		perr, dumpNote(projectDir, component, raw, s, perr))
}

func dumpNote(projectDir, component, raw, extracted string, cause error) string {
	if p := dumpProcessorOutput(projectDir, component, raw, extracted, cause); p != "" {
		return " — raw output saved to " + p
	}
	return ""
}

// repairJSON escapes the two things models get wrong inside JSON strings: a
// literal " that should have been \" and a raw control char. In valid JSON a
// quote inside a string is always either escaped or the terminator, and a
// terminator is always followed by , : } ] or the end — anything else means a
// missing backslash. So this is the identity function on valid JSON.
//
// ponytail: a heuristic, not a JSON parser. A stray quote sitting right before
// a comma (`say "hi", ok`) is indistinguishable from a terminator and still
// fails — deliberately, since guessing there could silently truncate a page.
// Those land in the dump file; if they ever show up in practice, the real fix
// is to stop shipping markdown inside JSON strings at all.
func repairJSON(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 16)
	inStr, esc := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case inStr && c == '\\':
			esc = true
		case c == '"':
			switch {
			case !inStr:
				inStr = true
			case closesString(s, i+1):
				inStr = false
			default:
				b.WriteString(`\"`)
				continue
			}
		case inStr && c < 0x20:
			b.WriteString(escapeControl(c))
			continue
		}
		b.WriteByte(c) // multibyte UTF-8 is all >= 0x80: copied through untouched
	}
	return b.String()
}

// closesString reports whether the quote before i is a real string terminator,
// i.e. whether what follows can legally follow a string in JSON.
func closesString(s string, i int) bool {
	for ; i < len(s); i++ {
		switch s[i] {
		case ' ', '\t', '\r', '\n':
		case ',', ':', '}', ']':
			return true
		default:
			return false
		}
	}
	return true // end of input
}

func escapeControl(c byte) string {
	switch c {
	case '\n':
		return `\n`
	case '\t':
		return `\t`
	case '\r':
		return `\r`
	case '\b':
		return `\b`
	case '\f':
		return `\f`
	}
	return fmt.Sprintf(`\u%04x`, c)
}

// dumpProcessorOutput saves the processor's raw text plus a window around the
// syntax error so a parse failure can be read after the fact — background runs
// have no stdout, and memoria.log collapses to 500 runes. One file per project,
// overwritten each run. Best-effort: never the cause of a failure.
func dumpProcessorOutput(projectDir, component, raw, extracted string, cause error) string {
	if projectDir == "" {
		return ""
	}
	p := filepath.Join(projectDir, ".memoria", "last-processor-error.txt")
	hdr := fmt.Sprintf("# memoria %s — %s\n# %v\n", component, time.Now().Format(time.RFC3339), cause)
	if ctx := offsetContext(extracted, cause); ctx != "" {
		hdr += ctx
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		logf(component, "dump: %v", err)
		return ""
	}
	if err := os.WriteFile(p, []byte(hdr+"\n"+raw), 0o644); err != nil {
		logf(component, "dump: %v", err)
		return ""
	}
	logf(component, "processor output saved to %s (%v)", p, cause)
	return p
}

// offsetContext turns the byte offset encoding/json reports into something
// readable: the 200 bytes either side of where the parse gave up.
func offsetContext(s string, cause error) string {
	if s == "" {
		return ""
	}
	var off int64
	var se *json.SyntaxError
	var ute *json.UnmarshalTypeError
	switch {
	case errors.As(cause, &se):
		off = se.Offset
	case errors.As(cause, &ute):
		off = ute.Offset
	default:
		return ""
	}
	if off < 0 || off > int64(len(s)) {
		return ""
	}
	lo := max(int(off)-200, 0)
	hi := min(int(off)+200, len(s))
	return fmt.Sprintf("# at offset %d of the extracted object:\n# ...%s...\n", off, collapse(s[lo:hi], 420))
}

// extractJSON tolerates fences/chatter around the object.
func extractJSON(s string) (string, error) {
	i := strings.Index(s, "{")
	j := strings.LastIndex(s, "}")
	if i < 0 || j <= i {
		return "", fmt.Errorf("no JSON object in processor output (%d bytes)", len(s))
	}
	return s[i : j+1], nil
}
