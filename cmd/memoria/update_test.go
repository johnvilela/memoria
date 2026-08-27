package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type fakeRelease struct {
	tag    string
	body   string
	binary []byte
	sum    string   // "" → correct sha256 of binary
	assets []string // nil → platform binary + checksums.txt
}

// updateEnv stubs the release API, TTY, picker, and target binary for one
// test: non-TTY, selectOption fails the test if reached (override per test).
// Returns the fake installed binary path, created with content "old".
func updateEnv(t *testing.T, rel fakeRelease) string {
	t.Helper()
	assetName := "memoria_" + runtime.GOOS + "_" + runtime.GOARCH
	if rel.assets == nil {
		rel.assets = []string{assetName, "checksums.txt"}
	}
	sum := rel.sum
	if sum == "" {
		sum = fmt.Sprintf("%x", sha256.Sum256(rel.binary))
	}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release":
			type asset struct {
				Name string `json:"name"`
				URL  string `json:"browser_download_url"`
			}
			assets := []asset{}
			for _, n := range rel.assets {
				assets = append(assets, asset{n, srv.URL + "/dl/" + n})
			}
			json.NewEncoder(w).Encode(map[string]any{"tag_name": rel.tag, "body": rel.body, "assets": assets})
		case "/dl/checksums.txt":
			fmt.Fprintf(w, "%s  %s\n", sum, assetName)
		case "/dl/" + assetName:
			w.Write(rel.binary)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	origURL := latestReleaseURL
	latestReleaseURL = srv.URL + "/release"
	t.Cleanup(func() { latestReleaseURL = origURL })

	exe := filepath.Join(t.TempDir(), "memoria")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	origExe := executable
	executable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { executable = origExe })

	origTTY := isTTY
	isTTY = func() bool { return false }
	t.Cleanup(func() { isTTY = origTTY })

	origSel := selectOption
	selectOption = func(title string, opts []option) (string, error) {
		t.Errorf("selectOption called unexpectedly: %q", title)
		return "no", nil
	}
	t.Cleanup(func() { selectOption = origSel })

	return exe
}

func mustBeUntouched(t *testing.T, exe string) {
	t.Helper()
	b, err := os.ReadFile(exe)
	if err != nil || string(b) != "old" {
		t.Fatalf("binary was touched: %q %v", b, err)
	}
}

func TestUpdateUpToDate(t *testing.T) {
	exe := updateEnv(t, fakeRelease{tag: "v" + version})
	var buf bytes.Buffer
	if code := runUpdate(nil, &buf); code != 0 {
		t.Fatalf("code = %d: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "up to date") {
		t.Fatalf("output = %q, want 'up to date'", buf.String())
	}
	mustBeUntouched(t, exe)
}

func TestUpdateLocalNewer(t *testing.T) {
	exe := updateEnv(t, fakeRelease{tag: "v0.0.1"})
	var buf bytes.Buffer
	if code := runUpdate(nil, &buf); code != 0 {
		t.Fatalf("code = %d: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "newer") {
		t.Fatalf("output = %q, want 'newer'", buf.String())
	}
	mustBeUntouched(t, exe)
}

func TestCmpVer(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.9.0", "0.9.0", 0},
		{"0.9.0", "0.10.0", -1},
		{"0.10.0", "0.9.0", 1},
		{"1.0.0", "0.99.99", 1},
		{"0.9.0", "0.9.1", -1},
	}
	for _, tc := range cases {
		if got := cmpVer(tc.a, tc.b); got != tc.want {
			t.Errorf("cmpVer(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestUpdateNonTTYHint(t *testing.T) {
	exe := updateEnv(t, fakeRelease{tag: "v99.0.0", body: "- fixed the flux capacitor"})
	var buf bytes.Buffer
	if code := runUpdate(nil, &buf); code != 0 {
		t.Fatalf("code = %d: %s", code, buf.String())
	}
	for _, want := range []string{"v99.0.0", "fixed the flux capacitor", "update -y"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("output = %q, missing %q", buf.String(), want)
		}
	}
	mustBeUntouched(t, exe)
}

func TestUpdateDeclined(t *testing.T) {
	exe := updateEnv(t, fakeRelease{tag: "v99.0.0"})
	isTTY = func() bool { return true }
	selectOption = func(title string, opts []option) (string, error) { return "no", nil }
	var buf bytes.Buffer
	if code := runUpdate(nil, &buf); code != 0 {
		t.Fatalf("code = %d: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "cancelled") {
		t.Fatalf("output = %q, want 'cancelled'", buf.String())
	}
	mustBeUntouched(t, exe)
}

func TestUpdateInstalls(t *testing.T) {
	exe := updateEnv(t, fakeRelease{tag: "v99.0.0", binary: []byte("new binary")})
	var buf bytes.Buffer
	if code := runUpdate([]string{"-y"}, &buf); code != 0 {
		t.Fatalf("code = %d: %s", code, buf.String())
	}
	b, err := os.ReadFile(exe)
	if err != nil || string(b) != "new binary" {
		t.Fatalf("binary = %q %v, want replaced", b, err)
	}
	info, _ := os.Stat(exe)
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %o, want 755", info.Mode().Perm())
	}
	if !strings.Contains(buf.String(), "Updated") {
		t.Fatalf("output = %q, want 'Updated'", buf.String())
	}
}

func TestUpdateChecksumMismatch(t *testing.T) {
	exe := updateEnv(t, fakeRelease{tag: "v99.0.0", binary: []byte("evil"), sum: strings.Repeat("0", 64)})
	var buf bytes.Buffer
	if code := runUpdate([]string{"-y"}, &buf); code != 1 {
		t.Fatalf("code = %d, want 1: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "checksum") {
		t.Fatalf("output = %q, want 'checksum'", buf.String())
	}
	mustBeUntouched(t, exe)
}

func TestUpdateNoAssetForPlatform(t *testing.T) {
	exe := updateEnv(t, fakeRelease{tag: "v99.0.0", assets: []string{"checksums.txt"}})
	var buf bytes.Buffer
	if code := runUpdate([]string{"-y"}, &buf); code != 1 {
		t.Fatalf("code = %d, want 1: %s", code, buf.String())
	}
	mustBeUntouched(t, exe)
}

func TestUpdateReplacesSymlinkTarget(t *testing.T) {
	exe := updateEnv(t, fakeRelease{tag: "v99.0.0", binary: []byte("new binary")})
	link := filepath.Join(filepath.Dir(exe), "link")
	if err := os.Symlink(exe, link); err != nil {
		t.Fatal(err)
	}
	executable = func() (string, error) { return link, nil }
	var buf bytes.Buffer
	if code := runUpdate([]string{"-y"}, &buf); code != 0 {
		t.Fatalf("code = %d: %s", code, buf.String())
	}
	if b, _ := os.ReadFile(exe); string(b) != "new binary" {
		t.Fatalf("target = %q, want replaced", b)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink was replaced by a regular file: %v %v", fi, err)
	}
}
