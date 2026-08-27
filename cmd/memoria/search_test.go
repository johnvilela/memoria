package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func searchFixture(t *testing.T) (proj, cfgPath string) {
	t.Helper()
	proj = t.TempDir()
	cfgPath = testConfig(t, proj)
	pages := map[string]string{
		"index.md":            "# Home\n\nwelcome page\n",
		"concepts/engine.md":  "---\ntags: [engine, rocket]\n---\n\n# Engine\n\nthe rocket engine burns fuel\n",
		"decisions/queue.md":  "---\ntags: [queue]\n---\n\n# Queue\n\nwe picked yaml over sqlite\n",
		"gotchas/hashtags.md": "# Hashtags\n\nbody mentions #engine literally\n",
	}
	for p, c := range pages {
		full := filepath.Join(proj, "wiki", p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return proj, cfgPath
}

func TestSearchWikiSubstring(t *testing.T) {
	wiki := map[string]string{
		"a.md": "The Rocket Engine\n",
		"b.md": "nothing here\n",
		"c.md": "small rocket\n",
	}
	got := searchWiki(wiki, "ROCKET")
	if len(got) != 2 || got[0] != "a.md" || got[1] != "c.md" {
		t.Fatalf("case-insensitive substring = %v, want [a.md c.md] sorted", got)
	}
	if got := searchWiki(wiki, "warp drive"); len(got) != 0 {
		t.Fatalf("no-match = %v, want empty", got)
	}
}

func TestSearchWikiHashtag(t *testing.T) {
	wiki := map[string]string{
		"tagged.md":  "---\ntags: [engine, rocket]\n---\n\n# T\n\nbody\n",
		"literal.md": "# L\n\nbody says #engine inline\n",
		"other.md":   "---\ntags: [queue]\n---\n\n# O\n\nbody\n",
	}
	got := searchWiki(wiki, "#engine")
	if len(got) != 1 || got[0] != "tagged.md" {
		t.Fatalf("#engine = %v, want [tagged.md] (frontmatter only, not body literals)", got)
	}
	if got := searchWiki(wiki, "#ENGINE"); len(got) != 1 {
		t.Fatalf("tag match should be case-insensitive, got %v", got)
	}
	if got := searchWiki(wiki, "#eng"); len(got) != 0 {
		t.Fatalf("tag match must be whole-tag, got %v", got)
	}
}

func TestRunSearchNonTTYListsMatches(t *testing.T) {
	proj, cfgPath := searchFixture(t)
	stubTTY(t, false)
	var buf bytes.Buffer
	if code := runSearch(proj, cfgPath, []string{"engine"}, &buf); code != 0 {
		t.Fatalf("non-tty multi-hit = %d, want 0: %s", code, buf.String())
	}
	if got, want := buf.String(), "concepts/engine.md\ngotchas/hashtags.md\n"; got != want {
		t.Fatalf("non-tty multi-hit should list sorted paths, got %q want %q", got, want)
	}
}

func TestRunSearchNonTTYSingleMatchPrintsContent(t *testing.T) {
	proj, cfgPath := searchFixture(t)
	stubTTY(t, false)
	var buf bytes.Buffer
	if code := runSearch(proj, cfgPath, []string{"sqlite"}, &buf); code != 0 {
		t.Fatalf("non-tty single-hit = %d, want 0: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "we picked yaml over sqlite") {
		t.Fatalf("non-tty single hit should print the page: %q", buf.String())
	}
}

func TestRunSearchNonTTYNoMatches(t *testing.T) {
	proj, cfgPath := searchFixture(t)
	stubTTY(t, false)
	var buf bytes.Buffer
	if code := runSearch(proj, cfgPath, []string{"warp", "drive"}, &buf); code != 1 {
		t.Fatalf("non-tty no matches = %d, want 1", code)
	}
}

func TestRunSearchGlobalFallback(t *testing.T) {
	cfgPath := testGlobalConfig(t, "")
	root := filepath.Dir(cfgPath)
	page := filepath.Join(root, "wiki", "srcfolder", "concepts", "queue.md")
	if err := os.MkdirAll(filepath.Dir(page), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(page, []byte("# Queue\n\nworkers pull jobs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stubTTY(t, false)
	var buf bytes.Buffer
	if code := runSearch(t.TempDir(), cfgPath, []string{"workers"}, &buf); code != 0 {
		t.Fatalf("global-mode search from unregistered cwd = %d, want 0: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "workers pull jobs") {
		t.Fatalf("should hit the global wiki: %q", buf.String())
	}
}

func TestRunSearchSingleMatchPrintsContent(t *testing.T) {
	proj, cfgPath := searchFixture(t)
	stubTTY(t, true)
	var buf bytes.Buffer
	if code := runSearch(proj, cfgPath, []string{"sqlite"}, &buf); code != 0 {
		t.Fatalf("search = %d: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "we picked yaml over sqlite") {
		t.Fatalf("single match should print the file without a selector: %q", buf.String())
	}
}

func TestRunSearchNoMatches(t *testing.T) {
	proj, cfgPath := searchFixture(t)
	stubTTY(t, true)
	var buf bytes.Buffer
	if code := runSearch(proj, cfgPath, []string{"warp", "drive"}, &buf); code != 1 {
		t.Fatalf("no matches = %d, want 1", code)
	}
	if !strings.Contains(buf.String(), "No matches") {
		t.Fatalf("output = %q", buf.String())
	}
}

func TestRunSearchEmptyQuery(t *testing.T) {
	proj, cfgPath := searchFixture(t)
	stubTTY(t, true)
	var buf bytes.Buffer
	if code := runSearch(proj, cfgPath, nil, &buf); code != 1 {
		t.Fatalf("empty query = %d, want 1", code)
	}
}

func TestRunSearchOutsideProject(t *testing.T) {
	_, cfgPath := searchFixture(t)
	stubTTY(t, true)
	var buf bytes.Buffer
	if code := runSearch(t.TempDir(), cfgPath, []string{"rocket"}, &buf); code != 1 {
		t.Fatalf("untracked cwd = %d, want 1", code)
	}
	if !strings.Contains(buf.String(), "not inside a tracked project") {
		t.Fatalf("output = %q", buf.String())
	}
}

func stubTTY(t *testing.T, val bool) {
	t.Helper()
	orig := isTTY
	isTTY = func() bool { return val }
	t.Cleanup(func() { isTTY = orig })
}
