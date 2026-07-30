package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const processorTimeout = 10 * time.Minute

// var so tests can point it at a httptest server
var geminiGenerateURL = "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent"

// invokeProcessor sends the prompt to the configured processor and returns
// its raw text output. dir is the project dir — codex runs there when it's a
// git repo (codex trusts git repos natively). Var so tests can stub the whole
// call.
var invokeProcessor = func(cfg config, dir, prompt string) (string, error) {
	switch cfg.Processor {
	case "claude-code":
		model := cfg.ProcessorModel
		if model == "" {
			model = "haiku" // wiki work is text digestion — cheap model suffices
		}
		return runProcessorCmd("claude", []string{"-p", "--model", model}, os.TempDir(), prompt)
	case "codex":
		model := cfg.ProcessorModel
		if model == "" {
			model = "gpt-5.4.mini"
		}
		effort := cfg.ProcessorEffort
		if effort == "" {
			effort = "high"
		}
		args := []string{"exec", "-m", model, "-c", "model_reasoning_effort=" + effort}
		if hasGitDir(dir) {
			return runProcessorCmd("codex", append(args, "-"), dir, prompt)
		}
		return runProcessorCmd("codex", append(args, "--skip-git-repo-check", "-"), os.TempDir(), prompt)
	case "gemini":
		return invokeGemini(cfg, prompt)
	case "ollama":
		return "", fmt.Errorf("ollama processor coming soon")
	case "":
		return "", fmt.Errorf("no processor configured — run memoria init")
	default:
		return "", fmt.Errorf("unknown processor %q", cfg.Processor)
	}
}

// hasGitDir reports whether dir contains a .git entry (dir or file — file for
// worktrees/submodules). Such dirs are trusted by codex without extra flags.
func hasGitDir(dir string) bool {
	if dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// runProcessorCmd executes an AI CLI with the prompt on stdin — argv has a
// ~128KiB per-arg kernel limit (E2BIG) that wiki+digest prompts easily blow.
// MEMORIA_NO_CAPTURE keeps the nested agent session out of memoria; claude
// additionally runs in a temp dir as belt-and-braces.
func runProcessorCmd(bin string, args []string, dir, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), processorTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "MEMORIA_NO_CAPTURE=1")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s: %w (%s)", bin, err, collapse(stderr.String(), 200))
	}
	return string(out), nil
}

func invokeGemini(cfg config, prompt string) (string, error) {
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		key = cfg.GeminiAPIKey
	}
	if key == "" {
		return "", fmt.Errorf("gemini needs an API key — set GEMINI_API_KEY or run memoria init")
	}
	body, err := json.Marshal(map[string]any{
		"contents": []map[string]any{{"parts": []map[string]string{{"text": prompt}}}},
	})
	if err != nil {
		return "", err
	}
	c := &http.Client{Timeout: processorTimeout}
	resp, err := c.Post(geminiGenerateURL+"?key="+url.QueryEscape(key), "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gemini: status %s", resp.Status)
	}
	var r struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	if len(r.Candidates) == 0 {
		return "", fmt.Errorf("gemini: no candidates in response")
	}
	var sb strings.Builder
	for _, p := range r.Candidates[0].Content.Parts {
		sb.WriteString(p.Text)
	}
	return strings.TrimSpace(sb.String()), nil
}
