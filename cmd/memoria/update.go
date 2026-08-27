package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// latestReleaseURL is a var so tests can point it at a httptest server.
var latestReleaseURL = "https://api.github.com/repos/johnvilela/memoria/releases/latest"

// executable is a var so tests replace a fake file, not the running test binary.
var executable = os.Executable

var updateOptions = []option{
	{"no", "No", "keep the current version"},
	{"yes", "Yes", "download and replace this binary"},
}

func runUpdate(args []string, out io.Writer) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(out)
	yes := fs.Bool("y", false, "install without asking")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(out, "usage: memoria update [-y]")
		return 1
	}

	client := &http.Client{Timeout: 2 * time.Minute}
	body, err := download(client, latestReleaseURL)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	var rel struct {
		TagName string `json:"tag_name"`
		Body    string `json:"body"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &rel); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}

	latest := strings.TrimPrefix(rel.TagName, "v")
	switch cmpVer(version, latest) {
	case 0:
		fmt.Fprintf(out, "memoria %s is up to date.\n", version)
		return 0
	case 1:
		fmt.Fprintf(out, "memoria %s is newer than the latest release %s — nothing to do.\n", version, rel.TagName)
		return 0
	}

	fmt.Fprintf(out, "New version available: %s (current %s)\n", rel.TagName, version)
	if notes := strings.TrimSpace(rel.Body); notes != "" {
		fmt.Fprintf(out, "\n%s\n\n", notes)
	}

	switch {
	case *yes:
	case !isTTY():
		fmt.Fprintln(out, "Run 'memoria update -y' to install.")
		return 0
	default:
		v, err := selectOption("Update to "+rel.TagName+"?", updateOptions)
		if err != nil || v != "yes" {
			fmt.Fprintln(out, "Update cancelled.")
			return 0
		}
	}

	assetName := fmt.Sprintf("memoria_%s_%s", runtime.GOOS, runtime.GOARCH)
	urls := map[string]string{}
	for _, a := range rel.Assets {
		urls[a.Name] = a.URL
	}
	if urls[assetName] == "" || urls["checksums.txt"] == "" {
		fmt.Fprintf(out, "error: release %s has no binary for %s/%s\n", rel.TagName, runtime.GOOS, runtime.GOARCH)
		return 1
	}

	fmt.Fprintf(out, "Downloading memoria %s...\n", rel.TagName)
	// ponytail: whole binary held in memory (~15 MB) — stream to the temp file if releases outgrow that
	bin, err := download(client, urls[assetName])
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	sums, err := download(client, urls["checksums.txt"])
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	want, err := expectedSum(sums, assetName)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(bin)); got != want {
		fmt.Fprintf(out, "error: checksum mismatch for %s\n", assetName)
		return 1
	}

	exe, err := executable()
	if err == nil {
		// replace the symlink's target, not the symlink itself
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	if err := installBinary(exe, bin); err != nil {
		fmt.Fprintf(out, "error: cannot replace %s: %v\n", exe, err)
		return 1
	}
	fmt.Fprintf(out, "Updated memoria %s → %s (%s)\n", version, latest, exe)
	return 0
}

// parseVer reads "x.y.z" into a comparable triple; malformed parts read as 0.
// ponytail: plain x.y.z only — the release ritual never emits pre-release tags
func parseVer(s string) (v [3]int) {
	fmt.Sscanf(s, "%d.%d.%d", &v[0], &v[1], &v[2])
	return
}

// cmpVer returns -1, 0, or 1 comparing a to b numerically per part.
func cmpVer(a, b string) int {
	va, vb := parseVer(a), parseVer(b)
	for i := range va {
		switch {
		case va[i] < vb[i]:
			return -1
		case va[i] > vb[i]:
			return 1
		}
	}
	return 0
}

func download(c *http.Client, url string) ([]byte, error) {
	resp, err := c.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// expectedSum returns the sha256 hex recorded for name in sha256sum-format
// output ("<hex>  <name>", "*" prefix on the name in binary mode).
func expectedSum(checksums []byte, name string) (string, error) {
	for _, line := range strings.Split(string(checksums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return f[0], nil
		}
	}
	return "", fmt.Errorf("no checksum for %s", name)
}

// installBinary atomically replaces exe: temp file in the same directory (so
// the rename never crosses devices), 0o755, then rename over the running
// binary — safe on unix, the running process keeps its inode.
func installBinary(exe string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(exe), "memoria-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), exe)
}
