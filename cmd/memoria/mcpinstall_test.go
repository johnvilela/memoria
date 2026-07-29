package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallMCPClaude(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	existing := `{
  "theme": "dark",
  "mcpServers": {"other": {"type": "stdio", "command": "/bin/other"}},
  "projects": {"/p": {"mcpServers": {}}}
}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installMCPClaude(path, "/usr/local/bin/memoria"); err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["theme"] != "dark" {
		t.Fatal("unknown key dropped")
	}
	servers := raw["mcpServers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Fatal("existing server dropped")
	}
	mem := servers["memoria"].(map[string]any)
	if mem["type"] != "stdio" || mem["command"] != "/usr/local/bin/memoria" {
		t.Fatalf("memoria entry = %v", mem)
	}
	if args := mem["args"].([]any); len(args) != 1 || args[0] != "mcp" {
		t.Fatalf("args = %v", mem["args"])
	}
	if proj := raw["projects"].(map[string]any)["/p"].(map[string]any); len(proj["mcpServers"].(map[string]any)) != 0 {
		t.Fatal("project-scope mcpServers touched")
	}

	// re-run with a moved binary re-points the command
	if err := installMCPClaude(path, "/new/memoria"); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	_ = json.Unmarshal(b, &raw)
	mem = raw["mcpServers"].(map[string]any)["memoria"].(map[string]any)
	if mem["command"] != "/new/memoria" {
		t.Fatalf("command not re-pointed: %v", mem)
	}
}

func TestInstallMCPClaudeCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	if err := installMCPClaude(path, "/bin/memoria"); err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["mcpServers"].(map[string]any)["memoria"]; !ok {
		t.Fatalf("memoria missing: %s", b)
	}
}

func TestInstallMCPCodex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	// missing file → created with the table
	if err := installMCPCodex(path, "/bin/memoria"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	want := "[mcp_servers.memoria]\ncommand = \"/bin/memoria\"\nargs = [\"mcp\"]\n"
	if !strings.Contains(string(b), want) {
		t.Fatalf("table missing:\n%s", b)
	}

	// other content preserved, table appended
	other := "model = \"o3\"\n\n[mcp_servers.other]\ncommand = \"/bin/other\"\n"
	if err := os.WriteFile(path, []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installMCPCodex(path, "/bin/memoria"); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if !strings.Contains(string(b), "model = \"o3\"") || !strings.Contains(string(b), "[mcp_servers.other]") {
		t.Fatalf("existing content lost:\n%s", b)
	}
	if !strings.Contains(string(b), want) {
		t.Fatalf("table not appended:\n%s", b)
	}

	// stale section rewritten in place, following section intact
	stale := "[mcp_servers.memoria]\ncommand = \"/old/memoria\"\nargs = [\"mcp\"]\n\n[other]\nkey = 1\n"
	if err := os.WriteFile(path, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installMCPCodex(path, "/new/memoria"); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if strings.Contains(string(b), "/old/memoria") {
		t.Fatalf("stale command kept:\n%s", b)
	}
	if !strings.Contains(string(b), "command = \"/new/memoria\"") || !strings.Contains(string(b), "[other]\nkey = 1") {
		t.Fatalf("rewrite broke the file:\n%s", b)
	}
	if strings.Count(string(b), "[mcp_servers.memoria]") != 1 {
		t.Fatalf("duplicated table:\n%s", b)
	}
}

func TestInitInstallsMCP(t *testing.T) {
	home, _ := initEnv(t)
	if code, out := runInitCmd(t, "claude-code", "--processor", "ollama"); code != 0 {
		t.Fatalf("init = %d: %s", code, out)
	}
	b, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		t.Fatalf("~/.claude.json not written: %v", err)
	}
	if !strings.Contains(string(b), "\"memoria\"") {
		t.Fatalf("memoria server missing:\n%s", b)
	}

	home, _ = initEnv(t)
	if code, out := runInitCmd(t, "codex", "--processor", "ollama"); code != 0 {
		t.Fatalf("init codex = %d: %s", code, out)
	}
	b, err = os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("config.toml not written: %v", err)
	}
	if !strings.Contains(string(b), "[mcp_servers.memoria]") {
		t.Fatalf("memoria table missing:\n%s", b)
	}
}
