package main

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

// runSearch finds wiki pages by content substring, or by frontmatter tag when
// the query starts with '#', then lets the user pick one to print. Human-only:
// agents get the same search through the MCP memoria_search tool.
func runSearch(cwd, configPath string, args []string, out io.Writer) int {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(out)
	trash := fs.Bool("trash", false, "also search deleted pages under trash/")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	query := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if query == "" {
		fmt.Fprintln(out, "usage: memoria search [--trash] <text | #tag>")
		return 1
	}
	if !isTTY() {
		fmt.Fprintln(out, "error: search is interactive; run it in a terminal")
		return 1
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	proj := matchProject(cwd, cfg.Projects)
	if proj == "" {
		fmt.Fprintln(out, "error: not inside a tracked project (run memoria bootstrap first)")
		return 1
	}
	p := projectAt(cfg, proj)
	wikiName := p.Wiki
	if wikiName == "" {
		wikiName = "wiki"
	}
	wiki := readWikiTrash(filepath.Join(proj, wikiName), *trash)
	hits := searchWiki(wiki, query)
	if len(hits) == 0 {
		fmt.Fprintf(out, "No matches for %q\n", query)
		return 1
	}
	choice := hits[0]
	if len(hits) > 1 {
		opts := make([]option, len(hits))
		for i, h := range hits {
			opts[i] = option{value: h, label: h}
		}
		if choice, err = selectOption(fmt.Sprintf("%d matches for %q", len(hits), query), opts); err != nil {
			return 1
		}
	}
	fmt.Fprint(out, wiki[choice])
	return 0
}

// readWikiTrash is readWiki plus, on request, the trash/ subtree readWiki
// deliberately hides — trashed pages come back keyed trash/<orig-path>.
func readWikiTrash(root string, includeTrash bool) map[string]string {
	wiki := readWiki(root)
	if includeTrash {
		for p, c := range readWiki(filepath.Join(root, "trash")) {
			wiki["trash/"+p] = c
		}
	}
	return wiki
}

// searchWiki returns the wiki-relative paths matching the query, sorted.
// '#tag' matches whole frontmatter tags; anything else is a case-insensitive
// content substring.
func searchWiki(wiki map[string]string, query string) []string {
	var hits []string
	if tag, isTag := strings.CutPrefix(query, "#"); isTag {
		for path, content := range wiki {
			for _, t := range pageTags(content) {
				if strings.EqualFold(t, tag) {
					hits = append(hits, path)
					break
				}
			}
		}
	} else {
		q := strings.ToLower(query)
		for path, content := range wiki {
			if strings.Contains(strings.ToLower(content), q) {
				hits = append(hits, path)
			}
		}
	}
	sort.Strings(hits)
	return hits
}

// pageTags parses `tags: [a, b]` out of a page's YAML frontmatter.
// ponytail: line-based parse of the one shape memoria writes; yaml lib if
// hand-edited frontmatter ever needs more.
func pageTags(content string) []string {
	rest, ok := strings.CutPrefix(content, "---\n")
	if !ok {
		return nil
	}
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil
	}
	for _, line := range strings.Split(rest[:end], "\n") {
		v, ok := strings.CutPrefix(line, "tags:")
		if !ok {
			continue
		}
		var tags []string
		for _, t := range strings.Split(strings.Trim(strings.TrimSpace(v), "[]"), ",") {
			if t = strings.TrimSpace(t); t != "" {
				tags = append(tags, t)
			}
		}
		return tags
	}
	return nil
}
