package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// bare --cron rewrites to this preset; flows through toOnCalendar like any other
const cronDefault = "8 times a day"

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

// systemdUserDir is where user units live — XDG-aware like the config.
func systemdUserDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "systemd", "user"), nil
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

// applyCronSetting is the shared install/uninstall path for init and setup.
// spec "" reuses the stored schedule (the --cron-apply-only path); "off" or
// "disabled" removes the timer. applySet false preserves the stored apply
// mode. systemctl failures are warn-only — units + config are the state.
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
	cal, err := toOnCalendar(spec)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	dir, err := systemdUserDir()
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	bin, err := os.Executable()
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	cmd := "process --all"
	if apply {
		cmd += " --apply"
	}
	service := "[Unit]\nDescription=memoria: consolidate agent sessions into project wikis\n\n" +
		"[Service]\nType=oneshot\nExecStart=\"" + bin + "\" " + cmd + "\n"
	timer := "[Unit]\nDescription=memoria: periodic session processing\n\n" +
		"[Timer]\nOnCalendar=" + cal + "\nPersistent=true\n\n" +
		"[Install]\nWantedBy=timers.target\n"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	// memoria-owned units: unconditional overwrite is idempotent
	if err := os.WriteFile(filepath.Join(dir, "memoria-process.service"), []byte(service), 0o644); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	if err := os.WriteFile(filepath.Join(dir, "memoria-process.timer"), []byte(timer), 0o644); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	cfg.Cron, cfg.CronApply = spec, apply
	if err := saveConfig(configPath, cfg); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	if err := runSystemctl("daemon-reload"); err != nil {
		fmt.Fprintln(out, "warning: systemctl daemon-reload failed:", err)
	}
	if err := runSystemctl("enable", "--now", "memoria-process.timer"); err != nil {
		fmt.Fprintln(out, "warning: could not enable the timer:", err)
	}
	mode := "proposals for review"
	if apply {
		mode = "auto-applied"
	}
	fmt.Fprintf(out, "Scheduled background processing: %s (OnCalendar=%s, %s)\n", spec, cal, mode)
	return 0
}

func removeCronTimer(cfg config, configPath string, out io.Writer) int {
	if err := runSystemctl("disable", "--now", "memoria-process.timer"); err != nil {
		fmt.Fprintln(out, "warning: could not disable the timer:", err)
	}
	dir, err := systemdUserDir()
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	for _, f := range []string{"memoria-process.service", "memoria-process.timer"} {
		if err := os.Remove(filepath.Join(dir, f)); err != nil && !os.IsNotExist(err) {
			fmt.Fprintln(out, "error:", err)
			return 1
		}
	}
	if err := runSystemctl("daemon-reload"); err != nil {
		fmt.Fprintln(out, "warning: systemctl daemon-reload failed:", err)
	}
	cfg.Cron, cfg.CronApply = "", false
	if err := saveConfig(configPath, cfg); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	fmt.Fprintln(out, "Background processing schedule removed")
	return 0
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
