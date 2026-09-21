package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type wikiGuideEntry struct {
	name, path, status, summary string
	pages                       int
}

// runGuide prints every wiki registered in memoria's config. It never scans
// arbitrary folders, so output stays deterministic and safe on large disks.
func runGuide(configPath string, out io.Writer) int {
	cfg, err := loadConfig(configPath)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(out, "No registered wikis.")
		return 0
	}
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}

	var entries []wikiGuideEntry
	seen := map[string]bool{}
	for _, project := range cfg.Projects {
		wiki := project.Wiki
		if wiki == "" {
			wiki = "wiki"
		}
		path := filepath.Clean(filepath.Join(project.Path, wiki))
		if seen[path] {
			continue
		}
		seen[path] = true
		entries = append(entries, inspectWiki(project.Name, path))
	}
	if cfg.Global {
		path := filepath.Clean(filepath.Join(globalRoot(cfg, configPath), "wiki"))
		if !seen[path] {
			entries = append(entries, inspectWiki(globalName, path))
		}
	}
	if len(entries) == 0 {
		fmt.Fprintln(out, "No registered wikis.")
		return 0
	}

	fmt.Fprintln(out, "Registered wikis:")
	for _, entry := range entries {
		fmt.Fprintf(out, "\n%s\n", entry.name)
		fmt.Fprintf(out, "  Path: %s\n", entry.path)
		fmt.Fprintf(out, "  Status: %s\n", entry.status)
		fmt.Fprintf(out, "  Pages: %d\n", entry.pages)
		fmt.Fprintf(out, "  Summary: %s\n", entry.summary)
	}
	return 0
}

func inspectWiki(name, path string) wikiGuideEntry {
	entry := wikiGuideEntry{name: name, path: path, status: "ready"}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		entry.status = "missing"
		entry.summary = "Wiki directory is missing."
		return entry
	}
	if err != nil {
		entry.status = "unavailable"
		entry.summary = err.Error()
		return entry
	}
	if !info.IsDir() {
		entry.status = "invalid"
		entry.summary = "Wiki path is not a directory."
		return entry
	}

	categories := map[string]bool{}
	walkErr := filepath.WalkDir(path, func(current string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && current != path {
			rel, _ := filepath.Rel(path, current)
			first := strings.Split(filepath.ToSlash(rel), "/")[0]
			if first == "trash" || strings.HasPrefix(first, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(d.Name()), ".md") {
			return nil
		}
		entry.pages++
		rel, _ := filepath.Rel(path, current)
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) > 1 {
			categories[parts[0]] = true
		}
		return nil
	})
	if walkErr != nil {
		entry.status = "unavailable"
		entry.summary = walkErr.Error()
		return entry
	}

	if b, err := os.ReadFile(filepath.Join(path, "index.md")); err == nil {
		entry.summary = firstProseParagraph(string(b))
	}
	if entry.summary == "" {
		entry.summary = pageCountSummary(entry.pages, categories)
	}
	return entry
}

func firstProseParagraph(markdown string) string {
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		lines = lines[1:]
		for len(lines) > 0 {
			line := strings.TrimSpace(lines[0])
			lines = lines[1:]
			if line == "---" {
				break
			}
		}
	}
	var paragraph []string
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			if len(paragraph) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			if len(paragraph) > 0 {
				break
			}
			continue
		}
		paragraph = append(paragraph, line)
	}
	summary := strings.Join(paragraph, " ")
	runes := []rune(summary)
	if len(runes) > 180 {
		summary = strings.TrimSpace(string(runes[:177])) + "..."
	}
	return summary
}

func pageCountSummary(pages int, categorySet map[string]bool) string {
	noun := "pages"
	if pages == 1 {
		noun = "page"
	}
	base := fmt.Sprintf("%d Markdown %s", pages, noun)
	var categories []string
	for category := range categorySet {
		categories = append(categories, category)
	}
	sort.Strings(categories)
	if len(categories) == 0 {
		return base + "."
	}
	return base + " across " + strings.Join(categories, ", ") + "."
}
