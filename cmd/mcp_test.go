package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tracked project with two wiki pages, a recorded session s1 and its pending digest
func mcpFixture(t *testing.T) (proj, cfgPath string) {
	t.Helper()
	proj = t.TempDir()
	cfgPath = testConfig(t, proj)
	pages := map[string]string{
		"index.md":          "# Index\n\nstart here\n",
		"concepts/queue.md": "---\ntags: [queue]\n---\n\n# Queue\n\nworkers pull jobs\n",
	}
	for p, c := range pages {
		dst := filepath.Join(proj, "wiki", filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(proj, ".memoria"), 0o755); err != nil {
		t.Fatal(err)
	}
	idx := "2026-07-28T10:00:00Z - s1 - add a queue\n"
	if err := os.WriteFile(filepath.Join(proj, ".memoria", "sessions.md"), []byte(idx), 0o644); err != nil {
		t.Fatal(err)
	}
	d := digestFile(proj, "s1")
	if err := os.MkdirAll(filepath.Dir(d), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d, []byte("---\nsession_id: s1\n---\n\n@user-prompt 'add a queue'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return proj, cfgPath
}

func TestMCPSearch(t *testing.T) {
	proj, cfgPath := mcpFixture(t)
	res, err := mcpSearch(proj, cfgPath, "workers pull", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matches) != 1 || res.Matches[0].Path != "concepts/queue.md" ||
		!strings.Contains(res.Matches[0].Content, "workers pull jobs") {
		t.Fatalf("matches = %+v", res.Matches)
	}
	if res, _ = mcpSearch(proj, cfgPath, "#queue", false); len(res.Matches) != 1 {
		t.Fatalf("tag search = %+v", res.Matches)
	}
	if res, _ = mcpSearch(proj, cfgPath, "nope-not-there", false); len(res.Matches) != 0 {
		t.Fatalf("want no matches, got %+v", res.Matches)
	}
	if _, err = mcpSearch(t.TempDir(), cfgPath, "x", false); err == nil {
		t.Fatal("untracked cwd must error")
	}
	if _, err = mcpSearch(proj, cfgPath, "", false); err == nil {
		t.Fatal("empty query must error")
	}
}

func TestMCPInstructions(t *testing.T) {
	for _, w := range []string{"memoria_search", "memoria_write_page", "memoria_recall", "@all"} {
		if !strings.Contains(mcpInstructions, w) {
			t.Fatalf("server instructions missing %q", w)
		}
	}
}

func TestMCPSearchCrossProject(t *testing.T) {
	alpha, _, cfgPath := crossFixture(t)
	res, err := mcpSearch(alpha, cfgPath, "@all engine", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matches) != 2 ||
		res.Matches[0].Project != "alpha" || res.Matches[0].Path != "concepts/engine.md" ||
		res.Matches[1].Project != "beta" || res.Matches[1].Path != "notes/motor.md" {
		t.Fatalf("@all matches = %+v, want alpha then beta ranked by score", res.Matches)
	}
	for _, m := range res.Matches {
		if m.Content == "" {
			t.Fatalf("content should inline on <=3 hits: %+v", m)
		}
	}
	if _, err := mcpSearch(alpha, cfgPath, "@nope engine", false); err == nil {
		t.Fatal("unknown project must error")
	}
	if _, err := mcpSearch(alpha, cfgPath, "@all", false); err == nil {
		t.Fatal("selector with no query must error")
	}
}

func TestMCPSearchOmitsContentOnManyHits(t *testing.T) {
	proj, cfgPath := mcpFixture(t)
	for _, p := range []string{"a", "b", "c", "d"} {
		dst := filepath.Join(proj, "wiki", "concepts", p+".md")
		if err := os.WriteFile(dst, []byte("shared needle\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res, err := mcpSearch(proj, cfgPath, "shared needle", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matches) != 4 {
		t.Fatalf("matches = %d", len(res.Matches))
	}
	for _, m := range res.Matches {
		if m.Content != "" {
			t.Fatalf("content included on %d hits: %+v", len(res.Matches), m)
		}
	}
}

func TestMCPRecall(t *testing.T) {
	proj, cfgPath := mcpFixture(t)
	res, err := mcpRecall(proj, cfgPath, "") // default = most recent session
	if err != nil {
		t.Fatal(err)
	}
	if res.SessionID != "s1" {
		t.Fatalf("sid = %q, want s1", res.SessionID)
	}
	for _, want := range []string{"# Session record: s1", "@user-prompt 'add a queue'", "## End of record"} {
		if !strings.Contains(res.Content, want) {
			t.Fatalf("content missing %q:\n%s", want, res.Content)
		}
	}
	for _, resumeOnly := range []string{"RESUMING", "Continue the work"} {
		if strings.Contains(res.Content, resumeOnly) {
			t.Fatalf("recall must not carry resume framing %q:\n%s", resumeOnly, res.Content)
		}
	}
	if _, err := os.Stat(filepath.Join(proj, "wiki", "sessions")); !os.IsNotExist(err) {
		t.Fatalf("recall must write nothing under wiki/sessions: %v", err)
	}
	if _, err := mcpRecall(proj, cfgPath, "../evil"); err == nil {
		t.Fatal("path-escaping sid must error")
	}
	if _, err := mcpRecall(proj, cfgPath, "nope"); err == nil {
		t.Fatal("unknown sid must error")
	}
}

func TestMCPWritePage(t *testing.T) {
	proj, cfgPath := mcpFixture(t)
	stubNow(t, "2026-08-31")
	res, err := mcpWritePage(proj, cfgPath, mcpWritePageIn{
		Path: "sessions/s1.md", Title: "S1", BodyMarkdown: "# S1\n\nnotes\n", Tags: []string{"a", "b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != "sessions/s1.md" || !res.Written {
		t.Fatalf("res = %+v", res)
	}
	b, _ := os.ReadFile(filepath.Join(proj, "wiki", "sessions", "s1.md"))
	if string(b) != "---\ntags: [a, b]\nlastUsed: 2026-08-31\n---\n\n# S1\n\nnotes\n" {
		t.Fatalf("page = %q", b)
	}
	for _, bad := range []mcpWritePageIn{
		{Path: "../evil.md", Title: "x", BodyMarkdown: "y"},
		{Path: "/abs.md", Title: "x", BodyMarkdown: "y"},
		{Path: "notes/x.md", Title: "x", BodyMarkdown: "y"}, // folder does not exist
		{Path: "trash/x.md", Title: "x", BodyMarkdown: "y"},
		{Path: "_global/x.md", Title: "x", BodyMarkdown: "y"},
		{Path: ".obsidian/x.md", Title: "x", BodyMarkdown: "y"},
		{Path: "concepts/x.md", Title: "", BodyMarkdown: ""},
	} {
		if _, err := mcpWritePage(proj, cfgPath, bad); err == nil {
			t.Fatalf("bad input accepted: %+v", bad)
		}
	}
	// existing custom folder is a valid target
	if err := os.MkdirAll(filepath.Join(proj, "wiki", "research"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err = mcpWritePage(proj, cfgPath, mcpWritePageIn{Path: "research/x.md", Title: "X", BodyMarkdown: "# X\n"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != "research/x.md" || !res.Written {
		t.Fatalf("res = %+v", res)
	}
}

func TestMCPDeletePage(t *testing.T) {
	proj, cfgPath := mcpFixture(t)
	res, err := mcpDeletePage(proj, cfgPath, "concepts/queue.md")
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != "trash/concepts/queue.md" || !res.Deleted {
		t.Fatalf("res = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(proj, "wiki", "concepts", "queue.md")); !os.IsNotExist(err) {
		t.Fatal("source still exists")
	}
	b, err := os.ReadFile(filepath.Join(proj, "wiki", "trash", "concepts", "queue.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "---\ntags: [queue, deleted]\n---") {
		t.Fatalf("deleted tag not added: %q", b)
	}
	// hidden from default search, visible with the flag
	if hits, _ := mcpSearch(proj, cfgPath, "workers pull", false); len(hits.Matches) != 0 {
		t.Fatalf("trashed page still searchable: %+v", hits.Matches)
	}
	hits, _ := mcpSearch(proj, cfgPath, "workers pull", true)
	if len(hits.Matches) != 1 || hits.Matches[0].Path != "trash/concepts/queue.md" {
		t.Fatalf("include_trash = %+v", hits.Matches)
	}

	if _, err := mcpDeletePage(proj, cfgPath, "concepts/queue.md"); err == nil {
		t.Fatal("missing page must error")
	}
	if _, err := mcpDeletePage(proj, cfgPath, "trash/concepts/queue.md"); err == nil {
		t.Fatal("trash path must error")
	}
	if _, err := mcpDeletePage(proj, cfgPath, "../evil.md"); err == nil {
		t.Fatal("traversal must error")
	}
}

func TestMCPDeletePageNoFrontmatterAndCollision(t *testing.T) {
	proj, cfgPath := mcpFixture(t)
	if _, err := mcpDeletePage(proj, cfgPath, "index.md"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(proj, "wiki", "trash", "index.md"))
	if !strings.HasPrefix(string(b), "---\ntags: [deleted]\n---\n\n# Index") {
		t.Fatalf("frontmatter not created: %q", b)
	}
	// same page written again, deleted again → suffixed, original kept
	if err := os.WriteFile(filepath.Join(proj, "wiki", "index.md"), []byte("second life\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := mcpDeletePage(proj, cfgPath, "index.md")
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != "trash/index-2.md" {
		t.Fatalf("collision path = %q", res.Path)
	}
	if b, _ := os.ReadFile(filepath.Join(proj, "wiki", "trash", "index.md")); !strings.Contains(string(b), "# Index") {
		t.Fatalf("first trashed copy clobbered: %q", b)
	}
}

func TestMCPDigestJob(t *testing.T) {
	proj, cfgPath := mcpFixture(t)
	spawned := stubSpawn(t, 4242)
	res, err := mcpDigest(proj, cfgPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "started" {
		t.Fatalf("state = %q", res.State)
	}
	want := []string{proj, "digest", "s1", "--foreground"}
	if strings.Join(*spawned, " ") != strings.Join(want, " ") {
		t.Fatalf("spawned %v, want %v", *spawned, want)
	}
	st, _ := loadStatus(statusPath(cfgPath))
	if st[filepath.Base(proj)].State != "running" {
		t.Fatalf("status = %+v", st[filepath.Base(proj)])
	}
	// poll while alive → running, no second spawn
	if err := statusSet(statusPath(cfgPath), filepath.Base(proj), "running", os.Getpid(), ""); err != nil {
		t.Fatal(err)
	}
	*spawned = nil
	if res, _ = mcpDigest(proj, cfgPath, ""); res.State != "running" || len(*spawned) != 0 {
		t.Fatalf("state = %q, spawned = %v", res.State, *spawned)
	}
	// job done → page content returned
	page := filepath.Join(proj, "wiki", "sessions", "s1.md")
	if err := os.MkdirAll(filepath.Dir(page), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(page, []byte("# S1\n\ncompiled\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := statusSet(statusPath(cfgPath), filepath.Base(proj), "done", 0, "session page written: sessions/s1.md"); err != nil {
		t.Fatal(err)
	}
	res, err = mcpDigest(proj, cfgPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "done" || res.Page != "sessions/s1.md" || !strings.Contains(res.Content, "compiled") {
		t.Fatalf("res = %+v", res)
	}
	// bad sid never reaches spawn
	if _, err := mcpDigest(proj, cfgPath, "../evil"); err == nil {
		t.Fatal("bad sid accepted")
	}
}

func TestMCPConsolidate(t *testing.T) {
	proj, cfgPath := mcpFixture(t)
	spawned := stubSpawn(t, 4242)
	// no ended sessions → idle, nothing spawned
	res, err := mcpConsolidate(proj, cfgPath, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "idle" || len(*spawned) != 0 {
		t.Fatalf("res = %+v, spawned = %v", res, *spawned)
	}
	// ended session → job starts
	d := digestFile(proj, "s1")
	if err := queueAdd(queuePath(cfgPath), filepath.Base(proj), d); err != nil {
		t.Fatal(err)
	}
	if err := queueMarkEnded(queuePath(cfgPath), filepath.Base(proj), d); err != nil {
		t.Fatal(err)
	}
	if res, _ = mcpConsolidate(proj, cfgPath, false, false); res.State != "started" {
		t.Fatalf("state = %q", res.State)
	}
	want := []string{proj, "process", "--foreground"}
	if strings.Join(*spawned, " ") != strings.Join(want, " ") {
		t.Fatalf("spawned %v, want %v", *spawned, want)
	}
	// proposal ready → done with page list
	prop := `{"project":"` + filepath.Base(proj) + `","sessions":["` + d + `"],"pages":[{"path":"concepts/queue.md","title":"Queue","tags":["queue"],"body_markdown":"# Queue\n\nnew content\n"}]}`
	if err := os.WriteFile(filepath.Join(proj, ".memoria", "proposal.json"), []byte(prop), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = mcpConsolidate(proj, cfgPath, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "done" || len(res.Pages) != 1 || res.Pages[0] != "concepts/queue.md" {
		t.Fatalf("res = %+v", res)
	}
	// apply writes the wiki and consumes the proposal
	res, err = mcpConsolidate(proj, cfgPath, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "done" || len(res.Pages) != 1 {
		t.Fatalf("apply res = %+v", res)
	}
	b, _ := os.ReadFile(filepath.Join(proj, "wiki", "concepts", "queue.md"))
	if !strings.Contains(string(b), "new content") {
		t.Fatalf("apply did not write: %q", b)
	}
	if _, err := os.Stat(filepath.Join(proj, ".memoria", "proposal.json")); !os.IsNotExist(err) {
		t.Fatal("proposal not consumed")
	}
	// apply without proposal → error
	if _, err := mcpConsolidate(proj, cfgPath, true, false); err == nil {
		t.Fatal("apply without proposal must error")
	}
}

func TestMCPLint(t *testing.T) {
	proj, cfgPath := mcpFixture(t)
	spawned := stubSpawn(t, 4242)
	res, err := mcpLint(proj, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "started" {
		t.Fatalf("state = %q", res.State)
	}
	want := []string{proj, "lint", "--foreground"}
	if strings.Join(*spawned, " ") != strings.Join(want, " ") {
		t.Fatalf("spawned %v, want %v", *spawned, want)
	}
	// clean run: done status, no report file
	if err := statusSet(statusPath(cfgPath), filepath.Base(proj), "done", 0, "lint: no conflicts found"); err != nil {
		t.Fatal(err)
	}
	if res, _ = mcpLint(proj, cfgPath); res.State != "done" || len(res.Findings) != 0 {
		t.Fatalf("clean res = %+v", res)
	}
	// report present → findings returned
	line := `{"kind":"contradiction","severity":"warning","message":"a vs b","pages":["index.md","concepts/queue.md"]}` + "\n"
	if err := os.WriteFile(filepath.Join(proj, ".memoria", "lint.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = mcpLint(proj, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "done" || len(res.Findings) != 1 || res.Findings[0].Kind != "contradiction" {
		t.Fatalf("res = %+v", res)
	}
}

func TestMCPBusySharedJobSlot(t *testing.T) {
	proj, cfgPath := mcpFixture(t)
	spawned := stubSpawn(t, 4242)
	if err := statusSet(statusPath(cfgPath), filepath.Base(proj), "running", os.Getpid(), ""); err != nil {
		t.Fatal(err)
	}
	d := digestFile(proj, "s1")
	if err := queueAdd(queuePath(cfgPath), filepath.Base(proj), d); err != nil {
		t.Fatal(err)
	}
	if err := queueMarkEnded(queuePath(cfgPath), filepath.Base(proj), d); err != nil {
		t.Fatal(err)
	}
	for name, call := range map[string]func() (mcpJobOut, error){
		"digest":      func() (mcpJobOut, error) { return mcpDigest(proj, cfgPath, "") },
		"consolidate": func() (mcpJobOut, error) { return mcpConsolidate(proj, cfgPath, false, false) },
		"lint":        func() (mcpJobOut, error) { return mcpLint(proj, cfgPath) },
	} {
		res, err := call()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if res.State != "running" || len(*spawned) != 0 {
			t.Fatalf("%s: res = %+v, spawned = %v", name, res, *spawned)
		}
	}
}

func TestSearchCLITrashFlag(t *testing.T) {
	proj, cfgPath := mcpFixture(t)
	stubTTY(t, true)
	if _, err := mcpDeletePage(proj, cfgPath, "concepts/queue.md"); err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	if code := runSearch(proj, cfgPath, []string{"workers", "pull"}, &buf); code != 1 {
		t.Fatalf("default search found trashed page: %s", buf.String())
	}
	buf.Reset()
	if code := runSearch(proj, cfgPath, []string{"--trash", "workers", "pull"}, &buf); code != 0 {
		t.Fatalf("search --trash = %d: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "workers pull jobs") {
		t.Fatalf("content not printed: %s", buf.String())
	}
}

func TestMCPConsolidateAutoApplied(t *testing.T) {
	proj, cfgPath := mcpFixture(t)
	spawned := stubSpawn(t, 4242)
	if err := statusSet(statusPath(cfgPath), filepath.Base(proj), "done", 0, "applied 2 pages from 1 sessions"); err != nil {
		t.Fatal(err)
	}
	res, err := mcpConsolidate(proj, cfgPath, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "done" || !strings.Contains(res.Detail, "applied") || len(*spawned) != 0 {
		t.Fatalf("res = %+v, spawned = %v", res, *spawned)
	}
}

// global capture on: wiki + recorded session g1 + pending digest under the
// global root (the config dir); calls come from an unregistered cwd
func mcpGlobalFixture(t *testing.T) (root, cwd, cfgPath string) {
	t.Helper()
	cfgPath = testGlobalConfig(t, "")
	root = filepath.Dir(cfgPath)
	pages := map[string]string{
		"index.md":                    "# Global index\n\nstart here\n",
		"srcfolder/concepts/queue.md": "---\ntags: [queue]\n---\n\n# Queue\n\nworkers pull jobs\n",
	}
	for p, c := range pages {
		dst := filepath.Join(root, "wiki", filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, ".memoria"), 0o755); err != nil {
		t.Fatal(err)
	}
	idx := "2026-07-28T10:00:00Z - g1 - fix the thing\n"
	if err := os.WriteFile(filepath.Join(root, ".memoria", "sessions.md"), []byte(idx), 0o644); err != nil {
		t.Fatal(err)
	}
	d := digestFile(root, "g1")
	if err := os.MkdirAll(filepath.Dir(d), 0o755); err != nil {
		t.Fatal(err)
	}
	obs := "---\nkind: session-digest\nsession_id: g1\nproject: srcfolder\nproject_root: /src/srcfolder\n---\n\n@user-prompt 'fix the thing'\n"
	if err := os.WriteFile(d, []byte(obs), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, t.TempDir(), cfgPath
}

func TestMCPGlobalDigest(t *testing.T) {
	root, cwd, cfgPath := mcpGlobalFixture(t)
	spawned := stubSpawn(t, 4242)
	res, err := mcpDigest(cwd, cfgPath, "g1") // explicit session id
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "started" {
		t.Fatalf("state = %q", res.State)
	}
	want := []string{cwd, "digest", "g1", "--foreground"}
	if strings.Join(*spawned, " ") != strings.Join(want, " ") {
		t.Fatalf("spawned %v, want %v", *spawned, want)
	}
	st, _ := loadStatus(statusPath(cfgPath))
	if st[globalName].State != "running" {
		t.Fatalf("status = %+v, want running under %s", st, globalName)
	}
	// default sid resolves from the global sessions.md; alive job → running
	if err := statusSet(statusPath(cfgPath), globalName, "running", os.Getpid(), ""); err != nil {
		t.Fatal(err)
	}
	*spawned = nil
	if res, _ = mcpDigest(cwd, cfgPath, ""); res.State != "running" || len(*spawned) != 0 {
		t.Fatalf("state = %q, spawned = %v", res.State, *spawned)
	}
	// job done → page content returned, default and explicit sid alike
	page := filepath.Join(root, "wiki", "sessions", "g1.md")
	if err := os.MkdirAll(filepath.Dir(page), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(page, []byte("# G1\n\ncompiled\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := statusSet(statusPath(cfgPath), globalName, "done", 0, "session page written: sessions/g1.md"); err != nil {
		t.Fatal(err)
	}
	for _, sid := range []string{"", "g1"} {
		res, err = mcpDigest(cwd, cfgPath, sid)
		if err != nil {
			t.Fatal(err)
		}
		if res.State != "done" || res.Page != "sessions/g1.md" || !strings.Contains(res.Content, "compiled") {
			t.Fatalf("sid %q: res = %+v", sid, res)
		}
	}
	// a repaired run's detail suffix must still read as done, not respawn
	if err := statusSet(statusPath(cfgPath), globalName, "done", 0, "session page written: sessions/g1.md — output repaired"); err != nil {
		t.Fatal(err)
	}
	*spawned = nil
	if res, _ = mcpDigest(cwd, cfgPath, "g1"); res.State != "done" || len(*spawned) != 0 {
		t.Fatalf("repaired detail: res = %+v, spawned = %v", res, *spawned)
	}
	if _, err := mcpDigest(cwd, cfgPath, "nope"); err == nil {
		t.Fatal("unknown sid must error")
	}
}

func TestMCPGlobalDigestGlobalOffErrors(t *testing.T) {
	cfgPath := testConfig(t)
	if _, err := mcpDigest(t.TempDir(), cfgPath, "g1"); err == nil ||
		!strings.Contains(err.Error(), "not inside a tracked project") {
		t.Fatalf("err = %v, want tracked-project error with global off", err)
	}
}

func TestMCPGlobalRecall(t *testing.T) {
	root, cwd, cfgPath := mcpGlobalFixture(t)
	for _, sid := range []string{"", "g1"} {
		res, err := mcpRecall(cwd, cfgPath, sid)
		if err != nil {
			t.Fatalf("sid %q: %v", sid, err)
		}
		if res.SessionID != "g1" || !strings.Contains(res.Content, "@user-prompt 'fix the thing'") {
			t.Fatalf("sid %q: res = %+v", sid, res)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "wiki", "sessions")); !os.IsNotExist(err) {
		t.Fatalf("recall must write nothing under wiki/sessions: %v", err)
	}
	if _, err := mcpRecall(cwd, cfgPath, "nope"); err == nil {
		t.Fatal("unknown sid must error")
	}
}

func TestMCPGlobalSearch(t *testing.T) {
	_, cwd, cfgPath := mcpGlobalFixture(t)
	res, err := mcpSearch(cwd, cfgPath, "workers pull", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matches) != 1 || res.Matches[0].Path != "srcfolder/concepts/queue.md" ||
		!strings.Contains(res.Matches[0].Content, "workers pull jobs") {
		t.Fatalf("matches = %+v", res.Matches)
	}
	if res, _ = mcpSearch(cwd, cfgPath, "#queue", false); len(res.Matches) != 1 {
		t.Fatalf("tag search = %+v", res.Matches)
	}
}

func TestMCPGlobalWriteDeletePage(t *testing.T) {
	root, cwd, cfgPath := mcpGlobalFixture(t)
	// a brand-new per-source-folder namespace is legal in the global wiki
	res, err := mcpWritePage(cwd, cfgPath, mcpWritePageIn{
		Path: "newfolder/concepts/x.md", Title: "X", BodyMarkdown: "# X\n\nnotes\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Written {
		t.Fatalf("res = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(root, "wiki", "newfolder", "concepts", "x.md")); err != nil {
		t.Fatalf("page not written under the global wiki: %v", err)
	}
	// reserved folders stay reserved in global mode
	for _, bad := range []string{"trash/x.md", "_global/x.md", ".obsidian/x.md", "../evil.md"} {
		if _, err := mcpWritePage(cwd, cfgPath, mcpWritePageIn{Path: bad, Title: "x", BodyMarkdown: "y"}); err == nil {
			t.Fatalf("bad path accepted: %q", bad)
		}
	}
	del, err := mcpDeletePage(cwd, cfgPath, "srcfolder/concepts/queue.md")
	if err != nil {
		t.Fatal(err)
	}
	if del.Path != "trash/srcfolder/concepts/queue.md" || !del.Deleted {
		t.Fatalf("res = %+v", del)
	}
	if _, err := mcpDeletePage(cwd, cfgPath, "../evil.md"); err == nil {
		t.Fatal("traversal must error")
	}
}

func TestMCPGlobalRegisteredProjectWins(t *testing.T) {
	proj := t.TempDir()
	cfgPath := testGlobalConfig(t, "", proj)
	root := filepath.Dir(cfgPath)
	if err := os.MkdirAll(filepath.Join(root, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	// inside the registered project: writes land in the project wiki, and the
	// global anything-goes namespace rule must not leak in
	if _, err := mcpWritePage(proj, cfgPath, mcpWritePageIn{Path: "concepts/a.md", Title: "A", BodyMarkdown: "# A\n"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(proj, "wiki", "concepts", "a.md")); err != nil {
		t.Fatalf("page not in the project wiki: %v", err)
	}
	if _, err := mcpWritePage(proj, cfgPath, mcpWritePageIn{Path: "newfolder/concepts/a.md", Title: "A", BodyMarkdown: "# A\n"}); err == nil {
		t.Fatal("global namespace rule leaked into a registered project")
	}
	// from an unregistered cwd: writes land in the global wiki
	if _, err := mcpWritePage(t.TempDir(), cfgPath, mcpWritePageIn{Path: "concepts/b.md", Title: "B", BodyMarkdown: "# B\n"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "wiki", "concepts", "b.md")); err != nil {
		t.Fatalf("page not in the global wiki: %v", err)
	}
	if _, err := os.Stat(filepath.Join(proj, "wiki", "concepts", "b.md")); !os.IsNotExist(err) {
		t.Fatal("global write leaked into the project wiki")
	}
}
