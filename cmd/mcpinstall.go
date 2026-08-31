package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// installMCPClaude registers the memoria MCP server at user scope in
// ~/.claude.json, preserving every other key (Claude Code keeps a lot of
// state in that file). Idempotent: re-runs re-point the command.
func installMCPClaude(path, bin string) error {
	raw := map[string]any{}
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(b, &raw); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
	case !errors.Is(err, os.ErrNotExist):
		return err
	}
	servers, _ := raw["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers["memoria"] = map[string]any{
		"type":    "stdio",
		"command": bin,
		"args":    []any{"mcp"},
	}
	raw["mcpServers"] = servers
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}

// installMCPCodex registers the server in ~/.codex/config.toml.
// ponytail: string surgery on the one table we own; TOML lib if it ever grows.
func installMCPCodex(path, bin string) error {
	const header = "[mcp_servers.memoria]"
	table := header + "\ncommand = " + fmt.Sprintf("%q", bin) + "\nargs = [\"mcp\"]\n"

	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	s := string(b)
	if i := strings.Index(s, header); i >= 0 {
		// replace our table's lines up to the next section header
		end := len(s)
		if j := strings.Index(s[i+len(header):], "\n["); j >= 0 {
			end = i + len(header) + j + 1
		}
		s = s[:i] + table + s[end:]
	} else {
		if s != "" && !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
		if s != "" {
			s += "\n"
		}
		s += table
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(s), 0o644)
}
