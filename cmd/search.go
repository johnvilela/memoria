package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// runSearch finds wiki pages by content terms, or by frontmatter tag when
// the query starts with '#', then lets the user pick one to print. Resolves
// like the MCP tools: project wiki inside a tracked project, global wiki
// elsewhere when global mode is on — unless the query leads with @project /
// @all selectors, which pick registered projects by name regardless of cwd.
// Headless (non-TTY) callers get the match list instead of the interactive
// picker.
func runSearch(cwd, configPath string, args []string, out io.Writer) int {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(out)
	trash := fs.Bool("trash", false, "also search deleted pages under trash/")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	sels, query := splitSelectors(strings.TrimSpace(strings.Join(fs.Args(), " ")))
	if query == "" {
		fmt.Fprintln(out, "usage: memoria search [--trash] [@project ...|@all] <text | #tag>")
		return 1
	}
	wss, err := searchWorkspaces(cwd, configPath, sels)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	hits := searchHits(wss, query, *trash)
	if len(hits) == 0 {
		fmt.Fprintf(out, "No matches for %q\n", query)
		return 1
	}
	// selector searches span projects, so their listings carry the
	// project:path prefix (the wiki's own cross-link vocabulary);
	// plain searches keep the bare-path output scripts rely on
	label := func(h searchHit) string {
		if len(sels) > 0 {
			return h.project + ":" + h.path
		}
		return h.path
	}
	choice := hits[0]
	if len(hits) > 1 {
		if !isTTY() {
			// headless callers get the ranked match list instead of a picker
			for _, h := range hits {
				fmt.Fprintln(out, label(h))
			}
			return 0
		}
		opts := make([]option, len(hits))
		for i, h := range hits {
			// value = index: labels aren't unique across same-named projects
			opts[i] = option{value: strconv.Itoa(i), label: label(h)}
		}
		v, err := selectOption(fmt.Sprintf("%d matches for %q", len(hits), query), opts)
		if err != nil {
			return 1
		}
		i, _ := strconv.Atoi(v)
		choice = hits[i]
	}
	// content delivery counts as usage; path-only listings above don't
	touchLastUsed(choice.wikiRoot, choice.path)
	fmt.Fprint(out, choice.content)
	return 0
}

// splitSelectors strips leading @project tokens off a query:
// "@a @b engine" → ([a b], "engine"). The first non-@ field ends selector
// parsing; a query with no leading @ comes back untouched.
func splitSelectors(query string) (sels []string, rest string) {
	fields := strings.Fields(query)
	i := 0
	for ; i < len(fields) && strings.HasPrefix(fields[i], "@"); i++ {
		sels = append(sels, strings.TrimPrefix(fields[i], "@"))
	}
	if i == 0 {
		return nil, query
	}
	return sels, strings.Join(fields[i:], " ")
}

// workspace is one wiki to search, tagged with its project name.
type workspace struct{ name, wikiRoot string }

// searchWorkspaces maps @selectors to the wikis to search. No selectors →
// the cwd's workspace, exactly like every other command. "all" overrides
// specific names and means every registered project (plus _global when
// global mode is on); names can collide across projects, so one selector
// keeps every project it names.
func searchWorkspaces(cwd, configPath string, sels []string) ([]workspace, error) {
	if len(sels) == 0 {
		_, _, name, wikiRoot, err := resolveWorkspace(cwd, configPath)
		if errors.Is(err, errNotTracked) {
			return nil, errors.New("not inside a tracked project (search with @all or @<project>, or run memoria bootstrap first)")
		}
		if err != nil {
			return nil, err
		}
		return []workspace{{name, wikiRoot}}, nil
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		return nil, err
	}
	candidates := cfg.Projects
	if cfg.Global {
		candidates = append(slices.Clone(candidates), globalProject(cfg, configPath))
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no projects registered (run memoria bootstrap first)")
	}
	var wss []workspace
	if slices.Contains(sels, "all") {
		for _, p := range candidates {
			wss = append(wss, workspace{p.Name, wikiRootFor(cfg, filepath.Clean(p.Path))})
		}
		return wss, nil
	}
	seen := map[string]bool{}
	for _, s := range sels {
		found := false
		for _, p := range candidates {
			if p.Name != s {
				continue
			}
			found = true
			root := wikiRootFor(cfg, filepath.Clean(p.Path))
			if !seen[root] {
				seen[root] = true
				wss = append(wss, workspace{p.Name, root})
			}
		}
		if !found {
			names := make([]string, len(candidates))
			for i, p := range candidates {
				names[i] = p.Name
			}
			return nil, fmt.Errorf("unknown project %q (known: %s)", s, strings.Join(names, ", "))
		}
	}
	return wss, nil
}

// searchHit is one match with everything delivery needs: the content to
// print and the owning wikiRoot so touchLastUsed stamps the right wiki.
type searchHit struct {
	project, wikiRoot, path, content string
	terms, score                     int
}

// searchHits runs the query over every workspace and merges the results into
// one relevance-ranked list (terms desc, score desc, then path, then
// project). When any page matched every term, the partial matches are noise
// and the list is cut down to the full matches.
func searchHits(wss []workspace, query string, trash bool) []searchHit {
	var hits []searchHit
	for _, ws := range wss {
		wiki := readWikiTrash(ws.wikiRoot, trash)
		for _, h := range searchWiki(wiki, query) {
			hits = append(hits, searchHit{ws.name, ws.wikiRoot, h.path, wiki[h.path], h.terms, h.score})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if a.terms != b.terms {
			return a.terms > b.terms
		}
		if a.score != b.score {
			return a.score > b.score
		}
		if a.path != b.path {
			return a.path < b.path
		}
		return a.project < b.project
	})
	if want := len(queryTerms(query)); len(hits) > 0 && hits[0].terms == want {
		for i, h := range hits {
			if h.terms < want {
				return hits[:i]
			}
		}
	}
	return hits
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

// scoredHit ranks a match: terms is how many distinct query terms the page
// contains, score the total occurrences; a tag hit pins both to 1.
type scoredHit struct {
	path         string
	terms, score int
}

// queryTerms splits a query into deduped lowercase terms; a #tag query is
// one opaque term.
func queryTerms(query string) []string {
	if strings.HasPrefix(query, "#") {
		return []string{query}
	}
	fields := strings.Fields(strings.ToLower(query))
	slices.Sort(fields)
	return slices.Compact(fields)
}

// searchWiki returns the wiki-relative paths matching the query, ranked by
// distinct terms matched (desc), then total occurrences (desc), then path.
// '#tag' matches whole frontmatter tags; anything else is split into
// case-insensitive substring terms — any term matches a page, and pages
// containing every term rank first (searchHits drops the partials when a
// full match exists).
func searchWiki(wiki map[string]string, query string) []scoredHit {
	var hits []scoredHit
	if tag, isTag := strings.CutPrefix(query, "#"); isTag {
		for path, content := range wiki {
			for _, t := range pageTags(content) {
				if strings.EqualFold(t, tag) {
					hits = append(hits, scoredHit{path, 1, 1})
					break
				}
			}
		}
	} else {
		terms := queryTerms(query)
		for path, content := range wiki {
			c := strings.ToLower(content)
			matched, occ := 0, 0
			for _, t := range terms {
				if n := strings.Count(c, t); n > 0 {
					matched++
					occ += n
				}
			}
			if matched > 0 {
				hits = append(hits, scoredHit{path, matched, occ})
			}
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].terms != hits[j].terms {
			return hits[i].terms > hits[j].terms
		}
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].path < hits[j].path
	})
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
