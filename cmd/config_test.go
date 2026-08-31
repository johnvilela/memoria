package main

import (
	"path/filepath"
	"testing"
)

func TestGlobalRootDefaultAndPath(t *testing.T) {
	configPath := "/home/u/.config/memoria/config.yaml"
	if got := globalRoot(config{}, configPath); got != "/home/u/.config/memoria" {
		t.Fatalf("default root = %q, want the config dir", got)
	}
	if got := globalRoot(config{GlobalPath: "/data/gwiki/"}, configPath); got != "/data/gwiki" {
		t.Fatalf("root = %q, want cleaned global_path", got)
	}
}

func TestResolveProjectRegisteredWinsOverGlobal(t *testing.T) {
	proj := t.TempDir()
	cfg := config{
		Global:   true,
		Projects: []project{{Name: filepath.Base(proj), Path: proj}},
	}
	p, ok := resolveProject(cfg, "/cfg/config.yaml", filepath.Join(proj, "sub"))
	if !ok || p.Name != filepath.Base(proj) || p.Path != proj {
		t.Fatalf("resolveProject = %+v, %v — want the registered project", p, ok)
	}
}

func TestResolveProjectGlobalFallback(t *testing.T) {
	cfg := config{Global: true}
	p, ok := resolveProject(cfg, "/cfg/config.yaml", "/somewhere/else")
	if !ok || p.Name != globalName || p.Path != "/cfg" {
		t.Fatalf("resolveProject = %+v, %v — want the global pseudo-project", p, ok)
	}
	if _, ok := resolveProject(config{}, "/cfg/config.yaml", "/somewhere/else"); ok {
		t.Fatal("resolved a project with global off in an unregistered cwd")
	}
}

func TestGlobalCommitCfg(t *testing.T) {
	// default root: memoria created the wiki repo purely to track it — always commit
	if got := globalCommitCfg(config{WikiAutoCommit: false}); !got.WikiAutoCommit {
		t.Fatal("default global root must force wiki_auto_commit on")
	}
	// --global-path: the user's folder — never touch git
	if got := globalCommitCfg(config{GlobalPath: "/data/g", WikiAutoCommit: true}); got.WikiAutoCommit {
		t.Fatal("global_path must force wiki_auto_commit off")
	}
}
