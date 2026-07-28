package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
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

// unitDir derives the isolated systemd user dir from an initEnv config path
func unitDir(cfgPath string) string {
	return filepath.Join(filepath.Dir(filepath.Dir(cfgPath)), "systemd", "user")
}

func systemctlCalled(calls [][]string, first string) bool {
	for _, c := range calls {
		if len(c) > 0 && c[0] == first {
			return true
		}
	}
	return false
}

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

func TestCronInstallWritesUnits(t *testing.T) {
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

func TestCronInvalidScheduleErrors(t *testing.T) {
	_, cfgPath := initEnv(t)
	stubSystemctl(t)
	code, out := runInitCmd(t, "claude-code", "--processor", "ollama", "--cron", "nonsense")
	if code != 1 {
		t.Fatalf("invalid schedule accepted: %d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(unitDir(cfgPath), "memoria-process.timer")); !os.IsNotExist(err) {
		t.Fatal("timer written despite invalid schedule")
	}
	cfg, _ := loadConfig(cfgPath)
	if cfg.Cron != "" {
		t.Fatalf("cron saved despite invalid schedule: %q", cfg.Cron)
	}
}

func TestCronSystemctlFailureWarnsOnly(t *testing.T) {
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

	// no stored schedule → error
	_, cfgPath2 := initEnv(t)
	if err := saveConfig(cfgPath2, config{Processor: "ollama"}); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if code := run([]string{"setup", "--cron-apply"}, strings.NewReader(""), &buf); code != 1 {
		t.Fatalf("cron-apply without schedule = %d: %s", code, buf.String())
	}
}

func TestInitCronOmittedPreservesConfig(t *testing.T) {
	_, cfgPath := initEnv(t)
	if err := saveConfig(cfgPath, config{Cron: "daily", CronApply: true}); err != nil {
		t.Fatal(err)
	}
	sc := stubSystemctl(t)
	if code, out := runInitCmd(t, "claude-code", "--processor", "ollama"); code != 0 {
		t.Fatalf("init = %d: %s", code, out)
	}
	cfg, _ := loadConfig(cfgPath)
	if cfg.Cron != "daily" || !cfg.CronApply {
		t.Fatalf("omitted --cron touched config: %+v", cfg)
	}
	if len(*sc) != 0 {
		t.Fatalf("systemctl called without --cron: %v", *sc)
	}
}
