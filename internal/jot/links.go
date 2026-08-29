package jot

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	markdownLinkPattern = regexp.MustCompile(`\[[^\]]*\]\(([^)[:space:]]+)`)
	// wikiLinkPattern matches Obsidian-style [[target]] and [[target|label]].
	wikiLinkPattern = regexp.MustCompile(`\[\[([^\]\|\n]+)(?:\|([^\]\n]*))?\]\]`)
	footnotePattern = regexp.MustCompile(`\[\^([^\]\s]+)\]`)
)

// Link is one outbound reference from a concept page.
type Link struct {
	Raw      string // the link target exactly as written
	Label    string
	TargetID string // resolved concept id, empty when external or unresolved
	External bool
	Wiki     bool // written as [[target]]
}

// LinkGraph holds resolved outbound and inbound edges across the whole wiki.
type LinkGraph struct {
	Out    map[string][]Link
	In     map[string][]string
	Issues []string
}

// Backlinks returns the sorted set of concept ids that link to id.
func (g *LinkGraph) Backlinks(id string) []string {
	seen := map[string]bool{}
	var out []string
	for _, from := range g.In[id] {
		if from == id || seen[from] {
			continue
		}
		seen[from] = true
		out = append(out, from)
	}
	sort.Strings(out)
	return out
}

// Orphans lists concepts with no inbound link from any other page.
func (g *LinkGraph) Orphans(docs []Document) []string {
	var out []string
	for _, d := range docs {
		if len(g.Backlinks(d.ID)) == 0 {
			out = append(out, d.ID)
		}
	}
	sort.Strings(out)
	return out
}

// resolveWikiLink maps a [[target]] to a concept id using, in order: an exact
// id match, a path relative to the linking page, then a unique basename match.
func resolveWikiLink(target, fromID string, byID map[string]bool, byBase map[string][]string) (string, bool) {
	target = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(target), ".md"))
	if target == "" {
		return "", false
	}
	if byID[target] {
		return target, true
	}
	rel := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(fromID), filepath.FromSlash(target))))
	if byID[rel] {
		return rel, true
	}
	if matches := byBase[strings.ToLower(filepath.Base(target))]; len(matches) == 1 {
		return matches[0], true
	}
	return "", false
}

// buildLinkGraph resolves every outbound link in the wiki and records the
// inverse edges. Broken and escaping links are reported as issues.
func buildLinkGraph(root string, docs []Document) *LinkGraph {
	g := &LinkGraph{Out: map[string][]Link{}, In: map[string][]string{}}
	byID := make(map[string]bool, len(docs))
	// A page is reachable by its slug and by its title. The candidate lists are
	// deduplicated because a page whose title matches its slug would otherwise
	// look ambiguous with itself.
	baseSets := map[string]map[string]bool{}
	addAlias := func(alias, id string) {
		alias = strings.ToLower(strings.TrimSpace(alias))
		if alias == "" {
			return
		}
		if baseSets[alias] == nil {
			baseSets[alias] = map[string]bool{}
		}
		baseSets[alias][id] = true
	}
	for _, d := range docs {
		byID[d.ID] = true
		addAlias(filepath.Base(d.ID), d.ID)
		addAlias(d.Title, d.ID)
	}
	byBase := make(map[string][]string, len(baseSets))
	for alias, ids := range baseSets {
		list := make([]string, 0, len(ids))
		for id := range ids {
			list = append(list, id)
		}
		sort.Strings(list)
		byBase[alias] = list
	}

	for _, d := range docs {
		var links []Link

		for _, match := range wikiLinkPattern.FindAllStringSubmatch(d.Body, -1) {
			target, label := match[1], match[1]
			if len(match) > 2 && match[2] != "" {
				label = match[2]
			}
			id, ok := resolveWikiLink(target, d.ID, byID, byBase)
			if !ok {
				g.Issues = append(g.Issues, fmt.Sprintf("%s: unresolved wiki link [[%s]]", d.Path, target))
				links = append(links, Link{Raw: target, Label: label, Wiki: true})
				continue
			}
			links = append(links, Link{Raw: target, Label: label, TargetID: id, Wiki: true})
			g.In[id] = append(g.In[id], d.ID)
		}

		for _, match := range markdownLinkPattern.FindAllStringSubmatch(d.Body, -1) {
			raw := strings.Trim(match[1], "<>")
			target := raw
			if target == "" || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			if strings.Contains(target, "://") {
				links = append(links, Link{Raw: raw, External: true})
				continue
			}
			if before, _, ok := strings.Cut(target, "#"); ok {
				target = before
			}
			if before, _, ok := strings.Cut(target, "?"); ok {
				target = before
			}
			if target == "" {
				continue
			}
			var resolved string
			if strings.HasPrefix(target, "/") {
				resolved = filepath.Join(root, "wiki", filepath.FromSlash(strings.TrimPrefix(target, "/")))
			} else {
				resolved = filepath.Join(root, filepath.Dir(filepath.FromSlash(d.Path)), filepath.FromSlash(target))
			}
			resolved = filepath.Clean(resolved)
			rel, err := filepath.Rel(root, resolved)
			if err != nil || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
				g.Issues = append(g.Issues, fmt.Sprintf("%s: link escapes vault: %s", d.Path, raw))
				continue
			}
			if _, err := os.Stat(resolved); err != nil {
				g.Issues = append(g.Issues, fmt.Sprintf("%s: broken link %s", d.Path, raw))
				links = append(links, Link{Raw: raw})
				continue
			}
			link := Link{Raw: raw}
			if wikiRel, err := filepath.Rel(filepath.Join(root, "wiki"), resolved); err == nil {
				id := strings.TrimSuffix(filepath.ToSlash(wikiRel), ".md")
				if byID[id] {
					link.TargetID = id
					g.In[id] = append(g.In[id], d.ID)
				}
			}
			links = append(links, link)
		}

		g.Out[d.ID] = links
	}
	sort.Strings(g.Issues)
	return g
}
