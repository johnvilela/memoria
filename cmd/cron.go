package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// bare --cron rewrites to this preset; flows through the schedule translators
// like any other
const cronDefault = "8 times a day"

// launchdLabel names the macOS LaunchAgent (plist basename + launchctl label).
const launchdLabel = "com.jv77.memoria.process"

var cronOptions = []option{
	{"disabled", "Disabled", "run memoria process manually"},
	{cronDefault, "8 times a day", "every 3 hours"},
	{"daily", "Daily", "once a day"},
	{"hourly", "Hourly", "every hour"},
	{"custom", "Custom", "cron expression or phrase like 'every 6 hours'"},
}

var cronApplyOptions = []option{
	{"review", "Proposals only", "review each proposal, then memoria process --apply"},
	{"apply", "Auto-apply", "wiki updates land without review"},
}

// runSystemctl shells to systemctl --user; var so tests stub it.
var runSystemctl = func(args ...string) error {
	return exec.Command("systemctl", append([]string{"--user"}, args...)...).Run()
}

// runLaunchctl shells to launchctl (macOS); var so tests stub it.
var runLaunchctl = func(args ...string) error {
	return exec.Command("launchctl", args...).Run()
}

// systemdUserDir is where user units live — XDG-aware like the config.
func systemdUserDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "systemd", "user"), nil
}

// launchAgentsDir is where per-user LaunchAgents live on macOS.
func launchAgentsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents"), nil
}

// normalizeCronArgs rewrites a bare --cron (next arg missing or another flag)
// to --cron=<cronDefault>, since stdlib flag has no optional-value strings.
func normalizeCronArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i, a := range args {
		if (a == "--cron" || a == "-cron") && (i+1 >= len(args) || strings.HasPrefix(args[i+1], "-")) {
			out = append(out, a+"="+cronDefault)
			continue
		}
		out = append(out, a)
	}
	return out
}

// toOnCalendar translates a schedule (systemd preset, human phrase or 5-field
// cron) into a systemd OnCalendar expression.
func toOnCalendar(spec string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(spec))
	switch s {
	case "hourly", "daily", "weekly":
		return s, nil
	}
	var n int
	if _, err := fmt.Sscanf(s, "every %d hours", &n); err == nil {
		if n < 1 || n > 23 {
			return "", fmt.Errorf("every %d hours: want 1-23", n)
		}
		return fmt.Sprintf("*-*-* 0/%d:00:00", n), nil
	}
	if _, err := fmt.Sscanf(s, "%d times a day", &n); err == nil {
		if n < 1 || 24%n != 0 {
			return "", fmt.Errorf("%d times a day: must divide 24", n)
		}
		return fmt.Sprintf("*-*-* 0/%d:00:00", 24/n), nil
	}
	f := strings.Fields(s)
	if len(f) != 5 {
		return "", fmt.Errorf("unrecognized schedule %q — use a 5-field cron expression, hourly/daily/weekly, 'every N hours' or 'N times a day'", spec)
	}
	min, err := convField(f[0], 0, 59)
	if err != nil {
		return "", fmt.Errorf("minute: %w", err)
	}
	hour, err := convField(f[1], 0, 23)
	if err != nil {
		return "", fmt.Errorf("hour: %w", err)
	}
	dom, err := convField(f[2], 1, 31)
	if err != nil {
		return "", fmt.Errorf("day of month: %w", err)
	}
	mon, err := convField(f[3], 1, 12)
	if err != nil {
		return "", fmt.Errorf("month: %w", err)
	}
	dowPart := ""
	if f[4] != "*" {
		names := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
		var parts []string
		for _, d := range strings.Split(f[4], ",") {
			v, err := strconv.Atoi(d)
			if err != nil || v < 0 || v > 7 {
				return "", fmt.Errorf("weekday %q: only numbers 0-7 supported", d)
			}
			parts = append(parts, names[v])
		}
		dowPart = strings.Join(parts, ",") + " "
	}
	return fmt.Sprintf("%s*-%s-%s %s:%s:00", dowPart, mon, dom, hour, min), nil
}

// convField converts one cron field to systemd syntax: "*", "n", "a,b,c",
// "*/n" → "0/n". ponytail: ranges (a-b) and names unsupported — clear error,
// add when someone asks.
func convField(f string, lo, hi int) (string, error) {
	if f == "*" {
		return "*", nil
	}
	if rest, ok := strings.CutPrefix(f, "*/"); ok {
		n, err := strconv.Atoi(rest)
		if err != nil || n < 1 || n > hi {
			return "", fmt.Errorf("bad step %q", f)
		}
		return fmt.Sprintf("0/%d", n), nil
	}
	var parts []string
	for _, p := range strings.Split(f, ",") {
		n, err := strconv.Atoi(p)
		if err != nil || n < lo || n > hi {
			return "", fmt.Errorf("bad value %q (want %d-%d; ranges and names unsupported)", p, lo, hi)
		}
		parts = append(parts, fmt.Sprintf("%02d", n))
	}
	return strings.Join(parts, ","), nil
}

// toCalendarIntervals translates a schedule into launchd StartCalendarInterval
// entries — the macOS counterpart of toOnCalendar. launchd has no step syntax,
// so "every N hours"/"N times a day"/`*/n` are expanded into explicit entries;
// a "*" field is emitted as a missing key (launchd treats it as a wildcard).
func toCalendarIntervals(spec string) ([]map[string]int, error) {
	s := strings.ToLower(strings.TrimSpace(spec))
	switch s {
	case "hourly":
		return []map[string]int{{"Minute": 0}}, nil
	case "daily":
		return []map[string]int{{"Hour": 0, "Minute": 0}}, nil
	case "weekly":
		return []map[string]int{{"Weekday": 1, "Hour": 0, "Minute": 0}}, nil // Monday, like systemd
	}
	var n int
	if _, err := fmt.Sscanf(s, "every %d hours", &n); err == nil {
		if n < 1 || n > 23 {
			return nil, fmt.Errorf("every %d hours: want 1-23", n)
		}
		return hourlyStepIntervals(n), nil
	}
	if _, err := fmt.Sscanf(s, "%d times a day", &n); err == nil {
		if n < 1 || 24%n != 0 {
			return nil, fmt.Errorf("%d times a day: must divide 24", n)
		}
		return hourlyStepIntervals(24 / n), nil
	}
	f := strings.Fields(s)
	if len(f) != 5 {
		return nil, fmt.Errorf("unrecognized schedule %q — use a 5-field cron expression, hourly/daily/weekly, 'every N hours' or 'N times a day'", spec)
	}
	mins, err := cronFieldValues(f[0], 0, 59)
	if err != nil {
		return nil, fmt.Errorf("minute: %w", err)
	}
	hours, err := cronFieldValues(f[1], 0, 23)
	if err != nil {
		return nil, fmt.Errorf("hour: %w", err)
	}
	dom, err := cronFieldValues(f[2], 1, 31)
	if err != nil {
		return nil, fmt.Errorf("day of month: %w", err)
	}
	mon, err := cronFieldValues(f[3], 1, 12)
	if err != nil {
		return nil, fmt.Errorf("month: %w", err)
	}
	dow, err := cronFieldValues(f[4], 0, 7)
	if err != nil {
		return nil, fmt.Errorf("weekday %q: only numbers 0-7 supported", f[4])
	}
	for i, v := range dow { // launchd: 0 and 7 both mean Sunday — normalize to 0
		if v == 7 {
			dow[i] = 0
		}
	}
	// cross-product the set axes; a nil axis ("*") contributes no key
	entries := []map[string]int{{}}
	for _, ax := range []struct {
		key  string
		vals []int
	}{{"Minute", mins}, {"Hour", hours}, {"Day", dom}, {"Month", mon}, {"Weekday", dow}} {
		if ax.vals == nil {
			continue
		}
		var next []map[string]int
		for _, e := range entries {
			for _, v := range ax.vals {
				ne := make(map[string]int, len(e)+1)
				for k, val := range e {
					ne[k] = val
				}
				ne[ax.key] = v
				next = append(next, ne)
			}
		}
		entries = next
	}
	return entries, nil
}

// hourlyStepIntervals fires at minute 0 of every step-th hour from midnight.
func hourlyStepIntervals(step int) []map[string]int {
	var out []map[string]int
	for h := 0; h < 24; h += step {
		out = append(out, map[string]int{"Hour": h, "Minute": 0})
	}
	return out
}

// cronFieldValues expands one cron field into explicit values, nil for "*".
// Steps ("*/n") and lists ("a,b,c") are enumerated; ranges and names are not.
func cronFieldValues(f string, lo, hi int) ([]int, error) {
	if f == "*" {
		return nil, nil
	}
	if rest, ok := strings.CutPrefix(f, "*/"); ok {
		n, err := strconv.Atoi(rest)
		if err != nil || n < 1 || n > hi {
			return nil, fmt.Errorf("bad step %q", f)
		}
		var vals []int
		for v := 0; v <= hi; v += n {
			if v >= lo {
				vals = append(vals, v)
			}
		}
		return vals, nil
	}
	var vals []int
	for _, p := range strings.Split(f, ",") {
		v, err := strconv.Atoi(p)
		if err != nil || v < lo || v > hi {
			return nil, fmt.Errorf("bad value %q (want %d-%d; ranges and names unsupported)", p, lo, hi)
		}
		vals = append(vals, v)
	}
	return vals, nil
}

// applyCronSetting is the shared install/uninstall path for init and setup.
// spec "" reuses the stored schedule (the --cron-apply-only path); "off" or
// "disabled" removes the timer. applySet false preserves the stored apply
// mode. The OS scheduler is picked by runtime.GOOS (systemd on linux, launchd
// on macOS); loader failures are warn-only — units + config are the state.
func applyCronSetting(spec string, apply, applySet bool, configPath string, out io.Writer) int {
	cfg, err := loadConfig(configPath)
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	if spec == "" {
		if cfg.Cron == "" {
			fmt.Fprintln(out, "error: no schedule configured — pass --cron <schedule> as well")
			return 1
		}
		spec = cfg.Cron
	}
	if !applySet {
		apply = cfg.CronApply
	}
	if spec == "off" || spec == "disabled" {
		return removeCronTimer(cfg, configPath, out)
	}
	bin, err := os.Executable()
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	args := []string{"process", "--all"}
	if apply {
		args = append(args, "--apply")
	}
	desc, err := installSchedule(spec, bin, args, out)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	cfg.Cron, cfg.CronApply = spec, apply
	if err := saveConfig(configPath, cfg); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	mode := "proposals for review"
	if apply {
		mode = "auto-applied"
	}
	fmt.Fprintf(out, "Scheduled background processing: %s (%s, %s)\n", spec, desc, mode)
	return 0
}

// installSchedule writes the OS-native scheduler artifact for spec and (re)loads
// it, returning a short description for the confirmation line. A bad schedule or
// unwritable target returns a non-nil error and the caller leaves config alone;
// loader failures (systemctl/launchctl) are warn-only.
func installSchedule(spec, bin string, args []string, out io.Writer) (string, error) {
	switch runtime.GOOS {
	case "linux":
		return installSystemdTimer(spec, bin, args, out)
	case "darwin":
		return installLaunchdAgent(spec, bin, args, out)
	default:
		return "", fmt.Errorf("scheduled processing is not supported on %s — run `memoria process --all` from your own scheduler", runtime.GOOS)
	}
}

// installSystemdTimer writes the memoria-process .service/.timer pair and
// enables the timer via systemctl --user.
func installSystemdTimer(spec, bin string, args []string, out io.Writer) (string, error) {
	cal, err := toOnCalendar(spec)
	if err != nil {
		return "", err
	}
	dir, err := systemdUserDir()
	if err != nil {
		return "", err
	}
	service := "[Unit]\nDescription=memoria: consolidate agent sessions into project wikis\n\n" +
		"[Service]\nType=oneshot\nExecStart=\"" + bin + "\" " + strings.Join(args, " ") + "\n"
	timer := "[Unit]\nDescription=memoria: periodic session processing\n\n" +
		"[Timer]\nOnCalendar=" + cal + "\nPersistent=true\n\n" +
		"[Install]\nWantedBy=timers.target\n"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// memoria-owned units: unconditional overwrite is idempotent
	if err := os.WriteFile(filepath.Join(dir, "memoria-process.service"), []byte(service), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "memoria-process.timer"), []byte(timer), 0o644); err != nil {
		return "", err
	}
	if err := runSystemctl("daemon-reload"); err != nil {
		fmt.Fprintln(out, "warning: systemctl daemon-reload failed:", err)
	}
	if err := runSystemctl("enable", "--now", "memoria-process.timer"); err != nil {
		fmt.Fprintln(out, "warning: could not enable the timer:", err)
	}
	return "OnCalendar=" + cal, nil
}

// installLaunchdAgent writes the LaunchAgent plist and (re)loads it via
// launchctl. The old copy is unloaded first so a schedule change takes effect.
func installLaunchdAgent(spec, bin string, args []string, out io.Writer) (string, error) {
	intervals, err := toCalendarIntervals(spec)
	if err != nil {
		return "", err
	}
	dir, err := launchAgentsDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, launchdLabel+".plist")
	if err := os.WriteFile(path, []byte(launchdPlist(launchdLabel, bin, args, intervals)), 0o644); err != nil {
		return "", err
	}
	// unload any prior copy (harmless if not loaded), then load the new one
	_ = runLaunchctl("unload", path)
	if err := runLaunchctl("load", "-w", path); err != nil {
		fmt.Fprintln(out, "warning: could not load the launchd agent:", err)
	}
	return fmt.Sprintf("StartCalendarInterval×%d", len(intervals)), nil
}

// launchdPlist renders a LaunchAgent property list for the process command.
func launchdPlist(label, bin string, args []string, intervals []map[string]int) string {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	b.WriteString("  <key>Label</key>\n  <string>" + xmlEscape(label) + "</string>\n")
	b.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	for _, a := range append([]string{bin}, args...) {
		b.WriteString("    <string>" + xmlEscape(a) + "</string>\n")
	}
	b.WriteString("  </array>\n")
	b.WriteString("  <key>StartCalendarInterval</key>\n")
	writeCalendarIntervals(&b, intervals)
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

// writeCalendarIntervals renders one <dict> for a single entry, or an <array>
// of dicts for several. Keys are emitted in a fixed order for determinism.
func writeCalendarIntervals(b *strings.Builder, intervals []map[string]int) {
	order := []string{"Minute", "Hour", "Day", "Month", "Weekday"}
	writeDict := func(indent string, m map[string]int) {
		b.WriteString(indent + "<dict>\n")
		for _, k := range order {
			if v, ok := m[k]; ok {
				fmt.Fprintf(b, "%s  <key>%s</key>\n%s  <integer>%d</integer>\n", indent, k, indent, v)
			}
		}
		b.WriteString(indent + "</dict>\n")
	}
	if len(intervals) == 1 {
		writeDict("  ", intervals[0])
		return
	}
	b.WriteString("  <array>\n")
	for _, m := range intervals {
		writeDict("    ", m)
	}
	b.WriteString("  </array>\n")
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func removeCronTimer(cfg config, configPath string, out io.Writer) int {
	if err := removeSchedule(out); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	cfg.Cron, cfg.CronApply = "", false
	if err := saveConfig(configPath, cfg); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	fmt.Fprintln(out, "Background processing schedule removed")
	return 0
}

// removeSchedule tears down the OS-native scheduler artifact. Loader failures
// are warn-only; only a failed file removal is a hard error.
func removeSchedule(out io.Writer) error {
	switch runtime.GOOS {
	case "linux":
		return removeSystemdTimer(out)
	case "darwin":
		return removeLaunchdAgent(out)
	default:
		return nil // nothing was installed
	}
}

func removeSystemdTimer(out io.Writer) error {
	if err := runSystemctl("disable", "--now", "memoria-process.timer"); err != nil {
		fmt.Fprintln(out, "warning: could not disable the timer:", err)
	}
	dir, err := systemdUserDir()
	if err != nil {
		return err
	}
	for _, f := range []string{"memoria-process.service", "memoria-process.timer"} {
		if err := os.Remove(filepath.Join(dir, f)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := runSystemctl("daemon-reload"); err != nil {
		fmt.Fprintln(out, "warning: systemctl daemon-reload failed:", err)
	}
	return nil
}

func removeLaunchdAgent(out io.Writer) error {
	dir, err := launchAgentsDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, launchdLabel+".plist")
	if err := runLaunchctl("unload", "-w", path); err != nil {
		fmt.Fprintln(out, "warning: could not unload the launchd agent:", err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// promptCron runs the interactive schedule + apply-mode selects. A non-empty
// keepLabel prepends a "keep current" escape (setup). chosen=false means the
// caller should leave config and units untouched.
func promptCron(keepLabel string) (spec string, apply, chosen bool, err error) {
	opts := cronOptions
	if keepLabel != "" {
		opts = append([]option{{"keep", keepLabel, ""}}, cronOptions...)
	}
	v, err := selectOption("Schedule background session processing?", opts)
	if err != nil {
		return "", false, false, err
	}
	switch v {
	case "keep":
		return "", false, false, nil
	case "disabled":
		return "off", false, true, nil
	case "custom":
		if spec, err = promptText("Schedule (cron or e.g. 'every 6 hours')"); err != nil || spec == "" {
			return "", false, false, err
		}
	default:
		spec = v
	}
	m, err := selectOption("Apply wiki updates automatically?", cronApplyOptions)
	if err != nil {
		return "", false, false, err
	}
	return spec, m == "apply", true, nil
}
