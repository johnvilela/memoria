package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// stubSystemctl records systemctl --user invocations
func stubSystemctl(t *testing.T) *[][]string {
	t.Helper()
	var got [][]string
	orig := runSystemctl
	runSystemctl = func(args ...string) error {
		got = append(got, args)
		return nil
	}
	t.Cleanup(func() { runSystemctl = orig })
	return &got
}

// stubLaunchctl records launchctl invocations
func stubLaunchctl(t *testing.T) *[][]string {
	t.Helper()
	var got [][]string
	orig := runLaunchctl
	runLaunchctl = func(args ...string) error {
		got = append(got, args)
		return nil
	}
	t.Cleanup(func() { runLaunchctl = orig })
	return &got
}

// unitDir derives the isolated systemd user dir from an initEnv config path
func unitDir(cfgPath string) string {
	return filepath.Join(filepath.Dir(filepath.Dir(cfgPath)), "systemd", "user")
}

// agentPlist derives the isolated LaunchAgent plist path from the temp HOME
func agentPlist(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist")
}

func loaderCalled(calls [][]string, verb string) bool {
	for _, c := range calls {
		if len(c) > 0 && c[0] == verb {
			return true
		}
	}
	return false
}

func systemctlCalled(calls [][]string, first string) bool { return loaderCalled(calls, first) }

func skipUnless(t *testing.T, goos string) {
	t.Helper()
	if runtime.GOOS != goos {
		t.Skipf("%s-specific scheduler test", goos)
	}
}

// --- pure translators: run on every OS ---

func TestToOnCalendar(t *testing.T) {
	ok := []struct{ in, want string }{
		{"hourly", "hourly"},
		{"daily", "daily"},
		{"weekly", "weekly"},
		{"8 times a day", "*-*-* 0/3:00:00"},
		{"3 times a day", "*-*-* 0/8:00:00"},
		{"every 4 hours", "*-*-* 0/4:00:00"},
		{"0 */3 * * *", "*-*-* 0/3:00:00"},
		{"30 8 * * *", "*-*-* 08:30:00"},
		{"0 8,20 * * *", "*-*-* 08,20:00:00"},
		{"*/15 * * * *", "*-*-* *:0/15:00"},
		{"0 9 * * 1", "Mon *-*-* 09:00:00"},
		{"0 9 * * 0", "Sun *-*-* 09:00:00"},
		{"0 9 * * 7", "Sun *-*-* 09:00:00"},
		{"0 9 1 1 *", "*-01-01 09:00:00"},
	}
	for _, tc := range ok {
		got, err := toOnCalendar(tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("toOnCalendar(%q) = %q, %v, want %q", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range []string{
		"5 times a day", // 24 not divisible
		"1-5 * * * *",   // ranges unsupported
		"61 * * * *",    // out of bounds
		"0 9 * * mon",   // names unsupported
		"0 9 * * */2",   // dow step unsupported
		"garbage", "every day", "* * *",
	} {
		if _, err := toOnCalendar(bad); err == nil {
			t.Fatalf("toOnCalendar(%q) accepted", bad)
		}
	}
}

func TestToCalendarIntervals(t *testing.T) {
	ok := []struct {
		in   string
		want []map[string]int
	}{
		{"hourly", []map[string]int{{"Minute": 0}}},
		{"daily", []map[string]int{{"Hour": 0, "Minute": 0}}},
		{"weekly", []map[string]int{{"Weekday": 1, "Hour": 0, "Minute": 0}}},
		{"every 4 hours", []map[string]int{
			{"Hour": 0, "Minute": 0}, {"Hour": 4, "Minute": 0}, {"Hour": 8, "Minute": 0},
			{"Hour": 12, "Minute": 0}, {"Hour": 16, "Minute": 0}, {"Hour": 20, "Minute": 0},
		}},
		{"0 8,20 * * *", []map[string]int{{"Minute": 0, "Hour": 8}, {"Minute": 0, "Hour": 20}}},
		{"30 8 * * *", []map[string]int{{"Minute": 30, "Hour": 8}}},
		{"0 9 * * 1", []map[string]int{{"Minute": 0, "Hour": 9, "Weekday": 1}}},
		{"0 9 * * 7", []map[string]int{{"Minute": 0, "Hour": 9, "Weekday": 0}}},
		{"0 9 1 1 *", []map[string]int{{"Minute": 0, "Hour": 9, "Day": 1, "Month": 1}}},
	}
	for _, tc := range ok {
		got, err := toCalendarIntervals(tc.in)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("toCalendarIntervals(%q) = %v, %v, want %v", tc.in, got, err, tc.want)
		}
	}
	// 8 times a day → every 3 hours → 8 entries from 0 to 21
	got, err := toCalendarIntervals("8 times a day")
	if err != nil || len(got) != 8 || got[0]["Hour"] != 0 || got[7]["Hour"] != 21 {
		t.Fatalf("8 times a day = %v, %v", got, err)
	}
	for _, bad := range []string{
		"5 times a day", "61 * * * *", "0 9 * * mon", "garbage", "every day", "* * *",
	} {
		if _, err := toCalendarIntervals(bad); err == nil {
			t.Fatalf("toCalendarIntervals(%q) accepted", bad)
		}
	}
}

func TestNormalizeCronArgs(t *testing.T) {
	cases := []struct{ in, want []string }{
		{[]string{"--cron"}, []string{"--cron=" + cronDefault}},
		{[]string{"-cron"}, []string{"-cron=" + cronDefault}},
		{[]string{"--cron", "--cron-apply"}, []string{"--cron=" + cronDefault, "--cron-apply"}},
		{[]string{"claude-code", "--cron"}, []string{"claude-code", "--cron=" + cronDefault}},
		{[]string{"--cron", "daily"}, []string{"--cron", "daily"}},
		{[]string{"--cron", "off"}, []string{"--cron", "off"}},
		{[]string{"--cron=daily"}, []string{"--cron=daily"}},
	}
	for _, tc := range cases {
		got := normalizeCronArgs(tc.in)
		if fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Fatalf("normalizeCronArgs(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// --- platform-agnostic install behavior ---

func TestCronInvalidScheduleErrors(t *testing.T) {
	_, cfgPath := initEnv(t)
	stubSystemctl(t)
	stubLaunchctl(t)
	code, out := runInitCmd(t, "claude-code", "--processor", "ollama", "--cron", "nonsense")
	if code != 1 {
		t.Fatalf("invalid schedule accepted: %d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(unitDir(cfgPath), "memoria-process.timer")); !os.IsNotExist(err) {
		t.Fatal("timer written despite invalid schedule")
	}
	if _, err := os.Stat(agentPlist(t)); !os.IsNotExist(err) {
		t.Fatal("plist written despite invalid schedule")
	}
	cfg, _ := loadConfig(cfgPath)
	if cfg.Cron != "" {
		t.Fatalf("cron saved despite invalid schedule: %q", cfg.Cron)
	}
}

func TestInitCronOmittedPreservesConfig(t *testing.T) {
	_, cfgPath := initEnv(t)
	if err := saveConfig(cfgPath, config{Cron: "daily", CronApply: true}); err != nil {
		t.Fatal(err)
	}
	sc := stubSystemctl(t)
	lc := stubLaunchctl(t)
	if code, out := runInitCmd(t, "claude-code", "--processor", "ollama"); code != 0 {
		t.Fatalf("init = %d: %s", code, out)
	}
	cfg, _ := loadConfig(cfgPath)
	if cfg.Cron != "daily" || !cfg.CronApply {
		t.Fatalf("omitted --cron touched config: %+v", cfg)
	}
	if len(*sc) != 0 || len(*lc) != 0 {
		t.Fatalf("scheduler touched without --cron: systemctl=%v launchctl=%v", *sc, *lc)
	}
}

func TestCronApplyOnlyNoScheduleErrors(t *testing.T) {
	_, cfgPath := initEnv(t)
	if err := saveConfig(cfgPath, config{Processor: "ollama"}); err != nil {
		t.Fatal(err)
	}
	stubSystemctl(t)
	stubLaunchctl(t)
	var buf bytes.Buffer
	if code := run([]string{"setup", "--cron-apply"}, strings.NewReader(""), &buf); code != 1 {
		t.Fatalf("cron-apply without schedule = %d: %s", code, buf.String())
	}
}

// --- systemd (Linux) ---

func TestCronInstallWritesUnits(t *testing.T) {
	skipUnless(t, "linux")
	_, cfgPath := initEnv(t)
	sc := stubSystemctl(t)
	code, out := runInitCmd(t, "claude-code", "--processor", "ollama", "--cron", "daily")
	if code != 0 {
		t.Fatalf("init = %d: %s", code, out)
	}
	svc, err := os.ReadFile(filepath.Join(unitDir(cfgPath), "memoria-process.service"))
	if err != nil {
		t.Fatal("service unit not written:", err)
	}
	if !strings.Contains(string(svc), "process --all") || strings.Contains(string(svc), "--apply") {
		t.Fatalf("service = %q, want process --all without --apply", svc)
	}
	tmr, err := os.ReadFile(filepath.Join(unitDir(cfgPath), "memoria-process.timer"))
	if err != nil {
		t.Fatal("timer unit not written:", err)
	}
	for _, w := range []string{"OnCalendar=daily", "Persistent=true", "WantedBy=timers.target"} {
		if !strings.Contains(string(tmr), w) {
			t.Fatalf("timer = %q, missing %q", tmr, w)
		}
	}
	if !systemctlCalled(*sc, "daemon-reload") || !systemctlCalled(*sc, "enable") {
		t.Fatalf("systemctl calls = %v, want daemon-reload + enable", *sc)
	}
	cfg, _ := loadConfig(cfgPath)
	if cfg.Cron != "daily" || cfg.CronApply {
		t.Fatalf("config cron = %q apply=%v, want daily/false", cfg.Cron, cfg.CronApply)
	}
}

func TestCronApplyBakesApplyFlag(t *testing.T) {
	skipUnless(t, "linux")
	_, cfgPath := initEnv(t)
	stubSystemctl(t)
	code, out := runInitCmd(t, "claude-code", "--processor", "ollama", "--cron", "daily", "--cron-apply")
	if code != 0 {
		t.Fatalf("init = %d: %s", code, out)
	}
	svc, _ := os.ReadFile(filepath.Join(unitDir(cfgPath), "memoria-process.service"))
	if !strings.Contains(string(svc), "process --all --apply") {
		t.Fatalf("service = %q, want --all --apply", svc)
	}
	cfg, _ := loadConfig(cfgPath)
	if !cfg.CronApply {
		t.Fatal("cron_apply not saved")
	}
}

func TestCronBareDefault(t *testing.T) {
	skipUnless(t, "linux")
	_, cfgPath := initEnv(t)
	stubSystemctl(t)
	code, out := runInitCmd(t, "claude-code", "--processor", "ollama", "--cron")
	if code != 0 {
		t.Fatalf("init = %d: %s", code, out)
	}
	cfg, _ := loadConfig(cfgPath)
	if cfg.Cron != cronDefault {
		t.Fatalf("cron = %q, want %q", cfg.Cron, cronDefault)
	}
	tmr, _ := os.ReadFile(filepath.Join(unitDir(cfgPath), "memoria-process.timer"))
	if !strings.Contains(string(tmr), "OnCalendar=*-*-* 0/3:00:00") {
		t.Fatalf("timer = %q, want 8x/day OnCalendar", tmr)
	}
}

func TestCronOffUninstalls(t *testing.T) {
	skipUnless(t, "linux")
	_, cfgPath := initEnv(t)
	sc := stubSystemctl(t)
	if code, out := runInitCmd(t, "claude-code", "--processor", "ollama", "--cron", "daily"); code != 0 {
		t.Fatalf("install = %d: %s", code, out)
	}
	if code, out := runInitCmd(t, "claude-code", "--cron", "off"); code != 0 {
		t.Fatalf("off = %d: %s", code, out)
	}
	for _, f := range []string{"memoria-process.service", "memoria-process.timer"} {
		if _, err := os.Stat(filepath.Join(unitDir(cfgPath), f)); !os.IsNotExist(err) {
			t.Fatalf("%s still present after off", f)
		}
	}
	if !systemctlCalled(*sc, "disable") {
		t.Fatalf("systemctl calls = %v, want disable", *sc)
	}
	cfg, _ := loadConfig(cfgPath)
	if cfg.Cron != "" || cfg.CronApply {
		t.Fatalf("config not cleared: cron=%q apply=%v", cfg.Cron, cfg.CronApply)
	}
}

func TestCronSystemctlFailureWarnsOnly(t *testing.T) {
	skipUnless(t, "linux")
	_, cfgPath := initEnv(t)
	orig := runSystemctl
	runSystemctl = func(args ...string) error { return fmt.Errorf("no systemd here") }
	t.Cleanup(func() { runSystemctl = orig })
	code, out := runInitCmd(t, "claude-code", "--processor", "ollama", "--cron", "daily")
	if code != 0 {
		t.Fatalf("systemctl failure broke init: %d %s", code, out)
	}
	if !strings.Contains(out, "warning") {
		t.Fatalf("missing warning: %q", out)
	}
	if _, err := os.Stat(filepath.Join(unitDir(cfgPath), "memoria-process.timer")); err != nil {
		t.Fatal("units not written on systemctl failure:", err)
	}
	cfg, _ := loadConfig(cfgPath)
	if cfg.Cron != "daily" {
		t.Fatalf("cron not saved: %q", cfg.Cron)
	}
}

func TestCronApplyOnlyUsesStoredSchedule(t *testing.T) {
	skipUnless(t, "linux")
	_, cfgPath := initEnv(t)
	if err := saveConfig(cfgPath, config{Processor: "ollama", Cron: "daily"}); err != nil {
		t.Fatal(err)
	}
	stubSystemctl(t)
	var buf bytes.Buffer
	if code := run([]string{"setup", "--cron-apply"}, strings.NewReader(""), &buf); code != 0 {
		t.Fatalf("setup --cron-apply = %d: %s", code, buf.String())
	}
	svc, _ := os.ReadFile(filepath.Join(unitDir(cfgPath), "memoria-process.service"))
	if !strings.Contains(string(svc), "--all --apply") {
		t.Fatalf("service = %q, want --apply baked in", svc)
	}
}

// --- launchd (macOS) ---

func TestCronInstallWritesAgent(t *testing.T) {
	skipUnless(t, "darwin")
	_, cfgPath := initEnv(t)
	lc := stubLaunchctl(t)
	code, out := runInitCmd(t, "claude-code", "--processor", "ollama", "--cron", "daily")
	if code != 0 {
		t.Fatalf("init = %d: %s", code, out)
	}
	plist, err := os.ReadFile(agentPlist(t))
	if err != nil {
		t.Fatal("plist not written:", err)
	}
	s := string(plist)
	if !strings.Contains(s, "<string>process</string>") || !strings.Contains(s, "<string>--all</string>") {
		t.Fatalf("plist = %q, want process --all in ProgramArguments", s)
	}
	if strings.Contains(s, "--apply") {
		t.Fatalf("plist = %q, want no --apply", s)
	}
	if !strings.Contains(s, launchdLabel) || !strings.Contains(s, "StartCalendarInterval") {
		t.Fatalf("plist missing label/schedule: %q", s)
	}
	if !loaderCalled(*lc, "load") {
		t.Fatalf("launchctl calls = %v, want load", *lc)
	}
	cfg, _ := loadConfig(cfgPath)
	if cfg.Cron != "daily" || cfg.CronApply {
		t.Fatalf("config cron = %q apply=%v, want daily/false", cfg.Cron, cfg.CronApply)
	}
}

func TestCronApplyBakesApplyFlagLaunchd(t *testing.T) {
	skipUnless(t, "darwin")
	_, cfgPath := initEnv(t)
	stubLaunchctl(t)
	code, out := runInitCmd(t, "claude-code", "--processor", "ollama", "--cron", "daily", "--cron-apply")
	if code != 0 {
		t.Fatalf("init = %d: %s", code, out)
	}
	plist, _ := os.ReadFile(agentPlist(t))
	if !strings.Contains(string(plist), "<string>--apply</string>") {
		t.Fatalf("plist = %q, want --apply in ProgramArguments", plist)
	}
	cfg, _ := loadConfig(cfgPath)
	if !cfg.CronApply {
		t.Fatal("cron_apply not saved")
	}
}

func TestCronBareDefaultLaunchd(t *testing.T) {
	skipUnless(t, "darwin")
	_, cfgPath := initEnv(t)
	stubLaunchctl(t)
	code, out := runInitCmd(t, "claude-code", "--processor", "ollama", "--cron")
	if code != 0 {
		t.Fatalf("init = %d: %s", code, out)
	}
	cfg, _ := loadConfig(cfgPath)
	if cfg.Cron != cronDefault {
		t.Fatalf("cron = %q, want %q", cfg.Cron, cronDefault)
	}
	plist, _ := os.ReadFile(agentPlist(t))
	s := string(plist)
	// 8 times a day → 8 hourly entries; confirm the last (hour 21) is present
	if !strings.Contains(s, "<integer>21</integer>") || strings.Count(s, "<key>Hour</key>") != 8 {
		t.Fatalf("plist = %q, want 8 hourly entries incl. hour 21", s)
	}
}

func TestCronOffUninstallsLaunchd(t *testing.T) {
	skipUnless(t, "darwin")
	_, cfgPath := initEnv(t)
	lc := stubLaunchctl(t)
	if code, out := runInitCmd(t, "claude-code", "--processor", "ollama", "--cron", "daily"); code != 0 {
		t.Fatalf("install = %d: %s", code, out)
	}
	if code, out := runInitCmd(t, "claude-code", "--cron", "off"); code != 0 {
		t.Fatalf("off = %d: %s", code, out)
	}
	if _, err := os.Stat(agentPlist(t)); !os.IsNotExist(err) {
		t.Fatal("plist still present after off")
	}
	if !loaderCalled(*lc, "unload") {
		t.Fatalf("launchctl calls = %v, want unload", *lc)
	}
	cfg, _ := loadConfig(cfgPath)
	if cfg.Cron != "" || cfg.CronApply {
		t.Fatalf("config not cleared: cron=%q apply=%v", cfg.Cron, cfg.CronApply)
	}
}

func TestCronLaunchctlFailureWarnsOnly(t *testing.T) {
	skipUnless(t, "darwin")
	_, cfgPath := initEnv(t)
	orig := runLaunchctl
	runLaunchctl = func(args ...string) error { return fmt.Errorf("no launchd here") }
	t.Cleanup(func() { runLaunchctl = orig })
	code, out := runInitCmd(t, "claude-code", "--processor", "ollama", "--cron", "daily")
	if code != 0 {
		t.Fatalf("launchctl failure broke init: %d %s", code, out)
	}
	if !strings.Contains(out, "warning") {
		t.Fatalf("missing warning: %q", out)
	}
	if _, err := os.Stat(agentPlist(t)); err != nil {
		t.Fatal("plist not written on launchctl failure:", err)
	}
	cfg, _ := loadConfig(cfgPath)
	if cfg.Cron != "daily" {
		t.Fatalf("cron not saved: %q", cfg.Cron)
	}
}

func TestCronApplyOnlyUsesStoredScheduleLaunchd(t *testing.T) {
	skipUnless(t, "darwin")
	_, cfgPath := initEnv(t)
	if err := saveConfig(cfgPath, config{Processor: "ollama", Cron: "daily"}); err != nil {
		t.Fatal(err)
	}
	stubLaunchctl(t)
	var buf bytes.Buffer
	if code := run([]string{"setup", "--cron-apply"}, strings.NewReader(""), &buf); code != 0 {
		t.Fatalf("setup --cron-apply = %d: %s", code, buf.String())
	}
	plist, _ := os.ReadFile(agentPlist(t))
	if !strings.Contains(string(plist), "<string>--apply</string>") {
		t.Fatalf("plist = %q, want --apply baked in", plist)
	}
}
