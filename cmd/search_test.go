package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
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
		"c.md": "small rocket, big rocket\n",
	}
	got := searchWiki(wiki, "ROCKET")
	if len(got) != 2 || got[0].path != "c.md" || got[1].path != "a.md" {
		t.Fatalf("substring = %v, want [c.md a.md] ranked by occurrence count", got)
	}
	if got[0].score != 2 || got[1].score != 1 {
		t.Fatalf("scores = %v, want c.md=2 a.md=1", got)
	}
	if got := searchWiki(wiki, "warp drive"); len(got) != 0 {
		t.Fatalf("no-match = %v, want empty", got)
	}
	tied := searchWiki(map[string]string{"z.md": "rocket\n", "a.md": "rocket\n"}, "rocket")
	if len(tied) != 2 || tied[0].path != "a.md" || tied[1].path != "z.md" {
		t.Fatalf("equal scores should tie-break by path, got %v", tied)
	}
}

func TestSearchWikiMultiWord(t *testing.T) {
	wiki := map[string]string{
		"both.md":    "Rocket FUEL burns\n",
		"single.md":  "rocket rocket rocket\n",
		"neither.md": "nothing here\n",
	}
	got := searchWiki(wiki, "rocket fuel")
	if len(got) != 2 || got[0].path != "both.md" || got[1].path != "single.md" {
		t.Fatalf("multi-word = %v, want both.md (all terms) above single.md (partial, more occurrences)", got)
	}
}

func TestRunSearchMultiWordAND(t *testing.T) {
	proj, cfgPath := searchFixture(t)
	stubTTY(t, false)
	var buf bytes.Buffer
	if code := runSearch(proj, cfgPath, []string{"burns", "engine"}, &buf); code != 0 {
		t.Fatalf("multi-word = %d, want 0: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "the rocket engine burns fuel") {
		t.Fatalf("page with all terms should be the only hit: %q", buf.String())
	}
}

func TestRunSearchMultiWordFallback(t *testing.T) {
	proj, cfgPath := searchFixture(t)
	stubTTY(t, false)
	var buf bytes.Buffer
	if code := runSearch(proj, cfgPath, []string{"engine", "sqlite", "welcome"}, &buf); code != 0 {
		t.Fatalf("partial-only = %d, want 0: %s", code, buf.String())
	}
	// no page has all three terms → every partial match, ranked by
	// occurrences (engine.md has 3 "engine" hits) then path
	want := "concepts/engine.md\ndecisions/queue.md\ngotchas/hashtags.md\nindex.md\n"
	if got := buf.String(); got != want {
		t.Fatalf("fallback listing = %q, want %q", got, want)
	}
}

func TestRunSearchAllMultiWordFiltersAcrossProjects(t *testing.T) {
	alpha, _, cfgPath := crossFixture(t)
	stubTTY(t, false)
	var buf bytes.Buffer
	if code := runSearch(alpha, cfgPath, []string{"@all", "hums", "engine"}, &buf); code != 0 {
		t.Fatalf("@all multi-word = %d, want 0: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "the engine hums") || strings.Contains(buf.String(), "alpha:") {
		t.Fatalf("beta's full match should drop alpha's partial: %q", buf.String())
	}
}

func TestSearchWikiHashtag(t *testing.T) {
	wiki := map[string]string{
		"tagged.md":  "---\ntags: [engine, rocket]\n---\n\n# T\n\nbody\n",
		"literal.md": "# L\n\nbody says #engine inline\n",
		"other.md":   "---\ntags: [queue]\n---\n\n# O\n\nbody\n",
	}
	got := searchWiki(wiki, "#engine")
	if len(got) != 1 || got[0].path != "tagged.md" || got[0].score != 1 {
		t.Fatalf("#engine = %v, want [tagged.md] score 1 (frontmatter only, not body literals)", got)
	}
	if got := searchWiki(wiki, "#ENGINE"); len(got) != 1 {
		t.Fatalf("tag match should be case-insensitive, got %v", got)
	}
	if got := searchWiki(wiki, "#eng"); len(got) != 0 {
		t.Fatalf("tag match must be whole-tag, got %v", got)
	}
}

func TestSplitSelectors(t *testing.T) {
	cases := []struct {
		in   string
		sels []string
		rest string
	}{
		{"engine", nil, "engine"},
		{"@alpha engine", []string{"alpha"}, "engine"},
		{"@alpha @beta rocket fuel", []string{"alpha", "beta"}, "rocket fuel"},
		{"@all #tag", []string{"all"}, "#tag"},
		{"engine @alpha", nil, "engine @alpha"},
		{"@alpha", []string{"alpha"}, ""},
		{"", nil, ""},
	}
	for _, c := range cases {
		sels, rest := splitSelectors(c.in)
		if !slices.Equal(sels, c.sels) || rest != c.rest {
			t.Fatalf("splitSelectors(%q) = %v %q, want %v %q", c.in, sels, rest, c.sels, c.rest)
		}
	}
}

func TestSearchWorkspaces(t *testing.T) {
	base := t.TempDir()
	alpha, beta := filepath.Join(base, "alpha"), filepath.Join(base, "beta")
	cfgPath := testConfig(t, alpha, beta)

	ws, err := searchWorkspaces(alpha, cfgPath, nil)
	if err != nil || len(ws) != 1 || ws[0].name != "alpha" || ws[0].wikiRoot != filepath.Join(alpha, "wiki") {
		t.Fatalf("nil selectors should resolve cwd's workspace: %v %v", ws, err)
	}
	ws, err = searchWorkspaces(t.TempDir(), cfgPath, []string{"beta"})
	if err != nil || len(ws) != 1 || ws[0].wikiRoot != filepath.Join(beta, "wiki") {
		t.Fatalf("@beta from unregistered cwd = %v %v, want beta's wiki", ws, err)
	}
	ws, err = searchWorkspaces(alpha, cfgPath, []string{"all"})
	if err != nil || len(ws) != 2 {
		t.Fatalf("@all with global off = %v %v, want the 2 registered projects", ws, err)
	}
	if ws, err = searchWorkspaces(alpha, cfgPath, []string{"beta", "all"}); err != nil || len(ws) != 2 {
		t.Fatalf("@all should override other selectors: %v %v", ws, err)
	}
	_, err = searchWorkspaces(alpha, cfgPath, []string{"nope"})
	if err == nil || !strings.Contains(err.Error(), "unknown project") || !strings.Contains(err.Error(), "alpha") {
		t.Fatalf("unknown selector should list known names, got %v", err)
	}
}

func TestSearchWorkspacesGlobal(t *testing.T) {
	proj := t.TempDir()
	cfgPath := testGlobalConfig(t, "", proj)
	ws, err := searchWorkspaces(t.TempDir(), cfgPath, []string{"all"})
	if err != nil || len(ws) != 2 {
		t.Fatalf("@all with global on = %v %v, want project + _global", ws, err)
	}
	ws, err = searchWorkspaces(t.TempDir(), cfgPath, []string{"_global"})
	if err != nil || len(ws) != 1 || ws[0].wikiRoot != filepath.Join(filepath.Dir(cfgPath), "wiki") {
		t.Fatalf("@_global = %v %v, want the global wiki", ws, err)
	}
}

func TestSearchWorkspacesDuplicateNames(t *testing.T) {
	a, b := filepath.Join(t.TempDir(), "api"), filepath.Join(t.TempDir(), "api")
	cfg := config{Projects: []project{{Name: "api", Path: a}, {Name: "api", Path: b}}}
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := saveConfig(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	ws, err := searchWorkspaces(a, cfgPath, []string{"api"})
	if err != nil || len(ws) != 2 {
		t.Fatalf("a name shared by two projects should search both: %v %v", ws, err)
	}
}

// crossFixture registers two projects; "engine" appears 3x in alpha's page and
// 1x in beta's, so relevance ordering is observable.
func crossFixture(t *testing.T) (alpha, beta, cfgPath string) {
	t.Helper()
	base := t.TempDir()
	alpha, beta = filepath.Join(base, "alpha"), filepath.Join(base, "beta")
	pages := map[string]string{
		filepath.Join(alpha, "wiki", "concepts", "engine.md"): "# Engine\n\nthe engine is an engine\n",
		filepath.Join(beta, "wiki", "notes", "motor.md"):      "# Motor\n\nthe engine hums\n",
	}
	for p, c := range pages {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return alpha, beta, testConfig(t, alpha, beta)
}

func TestRunSearchAllProjects(t *testing.T) {
	alpha, _, cfgPath := crossFixture(t)
	stubTTY(t, false)
	var buf bytes.Buffer
	if code := runSearch(alpha, cfgPath, []string{"@all", "engine"}, &buf); code != 0 {
		t.Fatalf("@all search = %d, want 0: %s", code, buf.String())
	}
	if got, want := buf.String(), "alpha:concepts/engine.md\nbeta:notes/motor.md\n"; got != want {
		t.Fatalf("@all should list project:path ranked by score, got %q want %q", got, want)
	}
}

func TestRunSearchOtherProject(t *testing.T) {
	alpha, _, cfgPath := crossFixture(t)
	stubTTY(t, false)
	var buf bytes.Buffer
	if code := runSearch(alpha, cfgPath, []string{"@beta", "engine"}, &buf); code != 0 {
		t.Fatalf("@beta from alpha = %d, want 0: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "the engine hums") {
		t.Fatalf("single cross-project hit should print the page: %q", buf.String())
	}
}

func TestRunSearchSelectorFromUnregisteredCwd(t *testing.T) {
	_, _, cfgPath := crossFixture(t)
	stubTTY(t, false)
	var buf bytes.Buffer
	if code := runSearch(t.TempDir(), cfgPath, []string{"@alpha", "engine"}, &buf); code != 0 {
		t.Fatalf("selector search from untracked cwd = %d, want 0: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "the engine is an engine") {
		t.Fatalf("should hit alpha's page: %q", buf.String())
	}
}

func TestRunSearchUnknownProject(t *testing.T) {
	alpha, _, cfgPath := crossFixture(t)
	stubTTY(t, false)
	var buf bytes.Buffer
	if code := runSearch(alpha, cfgPath, []string{"@nope", "engine"}, &buf); code != 1 {
		t.Fatalf("unknown project = %d, want 1: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "unknown project") || !strings.Contains(buf.String(), "beta") {
		t.Fatalf("error should list known projects: %q", buf.String())
	}
}

func TestRunSearchSelectorOnlyUsage(t *testing.T) {
	alpha, _, cfgPath := crossFixture(t)
	stubTTY(t, false)
	var buf bytes.Buffer
	if code := runSearch(alpha, cfgPath, []string{"@all"}, &buf); code != 1 {
		t.Fatalf("selector with no query = %d, want 1: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "usage:") {
		t.Fatalf("output = %q", buf.String())
	}
}

func TestRunSearchAllTrash(t *testing.T) {
	alpha, beta, cfgPath := crossFixture(t)
	page := filepath.Join(beta, "wiki", "trash", "old.md")
	if err := os.MkdirAll(filepath.Dir(page), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(page, []byte("# Old\n\nburied neutrino notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stubTTY(t, false)
	var buf bytes.Buffer
	if code := runSearch(alpha, cfgPath, []string{"--trash", "@all", "neutrino"}, &buf); code != 0 {
		t.Fatalf("--trash @all = %d, want 0: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "buried neutrino notes") {
		t.Fatalf("should hit beta's trashed page: %q", buf.String())
	}
}

func TestRunSearchAllStampsLastUsed(t *testing.T) {
	alpha, beta, cfgPath := crossFixture(t)
	page := filepath.Join(beta, "wiki", "sessions", "s9.md")
	if err := os.MkdirAll(filepath.Dir(page), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(page, []byte("---\ntags: [s]\n---\n\n# S9\n\nquasar drift log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stubTTY(t, false)
	var buf bytes.Buffer
	if code := runSearch(alpha, cfgPath, []string{"@all", "quasar"}, &buf); code != 0 {
		t.Fatalf("@all sessions hit = %d, want 0: %s", code, buf.String())
	}
	b, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "lastUsed: ") {
		t.Fatalf("delivering a cross-project sessions page must stamp its own wiki, got:\n%s", b)
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
	if !strings.Contains(buf.String(), "@all") {
		t.Fatalf("error should point at the @all selector escape hatch: %q", buf.String())
	}
}

func stubTTY(t *testing.T, val bool) {
	t.Helper()
	orig := isTTY
	isTTY = func() bool { return val }
	t.Cleanup(func() { isTTY = orig })
}
