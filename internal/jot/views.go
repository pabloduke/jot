package jot

import (
	"encoding/json"
	"fmt"
	"html"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// page describes one rendered document for the layout shell.
type shell struct {
	Title    string // browser title
	PageID   string // concept id, when viewing one (drives recently-viewed)
	NavKey   string // which top-nav entry is current
	Sidebar  string // pre-rendered aside markup; empty renders a single column
	Body     string
	DocTitle string
}

func (s *wikiServer) render(w http.ResponseWriter, sh shell) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	navItems := []struct{ key, href, label string }{
		{"index", "/", "Index"},
		{"recent", "/recent", "Recent"},
		{"loose", "/loose-ends", "Loose ends"},
		{"tags", "/tags", "Tags"},
		{"graph", "/graph", "Graph"},
		{"log", "/log", "Log"},
	}
	var nav strings.Builder
	for _, item := range navItems {
		current := ""
		if item.key == sh.NavKey {
			current = ` aria-current="page"`
		}
		fmt.Fprintf(&nav, `<a href="%s"%s>%s</a>`, item.href, current, html.EscapeString(item.label))
	}

	shellClass := "shell"
	aside := ""
	if sh.Sidebar == "" {
		shellClass = "shell solo"
	} else {
		aside = `<aside>` + sh.Sidebar + `</aside>`
	}

	fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8">`+
		`<meta name="viewport" content="width=device-width,initial-scale=1">`+
		`<meta name="robots" content="noindex,nofollow"><meta name="theme-color" content="#1f1c18">`+
		`<title>%s</title><link rel="icon" type="image/svg+xml" href="/assets/favicon.svg">`+
		`<link rel="stylesheet" href="/assets/wiki.css"></head>`+
		`<body data-page="%s" data-title="%s"><header><div class="bar">`+
		`<a class="brand" href="/">jot</a><nav class="nav">%s</nav>`+
		`<form class="search" action="/search" autocomplete="off"><input type="search" name="q" aria-label="Search wiki" placeholder="Search…"><button>Search</button></form>`+
		`<button id="theme" type="button">Theme</button>`+
		`</div></header><div class="%s">%s<main>%s</main></div>`+
		`<footer>Read-only view of the local compiled wiki. Raw captures are not exposed. `+
		`Press <kbd>?</kbd> for keyboard shortcuts.</footer>%s`+
		`<script src="/assets/wiki.js"></script></body></html>`,
		html.EscapeString(sh.Title), html.EscapeString(sh.PageID), html.EscapeString(sh.DocTitle),
		nav.String(), shellClass, aside, sh.Body, helpDialog())
}

func helpDialog() string {
	rows := [][2]string{
		{"/", "focus search"},
		{"j / k", "move through results"},
		{"Enter", "open the focused result"},
		{"g h", "index"}, {"g n", "recent"}, {"g e", "loose ends"},
		{"g t", "tags"}, {"g a", "graph"}, {"g l", "log"}, {"g r", "random page"},
		{"?", "this help"},
	}
	var out strings.Builder
	out.WriteString(`<dialog id="help"><h2 style="margin-top:0">Keyboard</h2><table>`)
	for _, r := range rows {
		fmt.Fprintf(&out, `<tr><td><kbd>%s</kbd></td><td>%s</td></tr>`,
			html.EscapeString(r[0]), html.EscapeString(r[1]))
	}
	out.WriteString(`</table><p class="meta">Esc closes.</p></dialog>`)
	return out.String()
}

// --- sidebar --------------------------------------------------------------

// sidebar renders the contextual rail: the current topic's siblings first,
// then the page's own headings, then the full topic tree collapsed, then the
// viewer's recently-read list which is filled in by the client.
func (s *wikiServer) sidebar(snap *siteSnapshot, currentID string, headings []Heading) string {
	var out strings.Builder

	if currentID != "" {
		if siblings := snap.siblings(currentID); len(siblings) > 1 {
			topic := topicOf(currentID)
			fmt.Fprintf(&out, `<section><h3>%s</h3><ul>`, html.EscapeString(topicLabel(topic)))
			for _, d := range siblings {
				current := ""
				if d.ID == currentID {
					current = ` aria-current="page"`
				}
				fmt.Fprintf(&out, `<li><a href="/wiki/%s"%s>%s</a></li>`,
					pathURL(d.ID), current, html.EscapeString(d.Title))
			}
			out.WriteString(`</ul></section>`)
		}
	}

	if len(headings) > 1 {
		out.WriteString(`<section class="toc"><h3>On this page</h3><ul>`)
		for _, h := range headings {
			fmt.Fprintf(&out, `<li><a class="l%d" href="#%s">%s</a></li>`,
				h.Level, html.EscapeString(h.Slug), html.EscapeString(h.Text))
		}
		out.WriteString(`</ul></section>`)
	}

	out.WriteString(`<section><details><summary>All topics</summary><ul class="tree">`)
	for _, topic := range snap.topics {
		fmt.Fprintf(&out, `<li><span>%s</span><ul>`, html.EscapeString(topicLabel(topic)))
		for _, d := range snap.byTopic[topic] {
			current := ""
			if d.ID == currentID {
				current = ` aria-current="page"`
			}
			fmt.Fprintf(&out, `<li><a href="/wiki/%s"%s>%s</a></li>`,
				pathURL(d.ID), current, html.EscapeString(d.Title))
		}
		out.WriteString(`</ul></li>`)
	}
	out.WriteString(`</ul></details></section>`)

	// Populated from localStorage; removed by the script when empty.
	out.WriteString(`<section id="recently-viewed"><h3>Recently read</h3></section>`)
	return out.String()
}

// --- handlers -------------------------------------------------------------

func (s *wikiServer) home(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path != "/" {
		http.NotFound(w, req)
		return
	}
	snap, err := s.site(req.Context())
	if err != nil {
		http.Error(w, "Cannot read wiki: "+err.Error(), http.StatusInternalServerError)
		return
	}
	now := nowUTC()

	var body strings.Builder
	body.WriteString(`<h1>Knowledge Wiki</h1>`)
	fmt.Fprintf(&body, `<p class="meta">%d concepts across %d topics.</p>`, len(snap.docs), len(snap.topics))

	if len(snap.docs) == 0 {
		body.WriteString(`<p class="empty">No compiled wiki pages yet. Add a capture and let an AI harness compile it.</p>`)
	} else {
		body.WriteString(`<section class="lane"><h2>Recently updated <span class="count">`)
		body.WriteString(html.EscapeString(recencySourceLabel(snap.recency)))
		body.WriteString(`</span></h2><ul class="rows">`)
		for _, item := range snap.recency.MostRecent(snap.docs, 8) {
			fmt.Fprintf(&body, `<li><a href="/wiki/%s">%s</a><span class="when">%s</span></li>`,
				pathURL(item.Doc.ID), html.EscapeString(item.Doc.Title), html.EscapeString(humanAge(item.Updated, now)))
		}
		body.WriteString(`</ul></section>`)
	}

	for _, topic := range snap.topics {
		fmt.Fprintf(&body, `<h2 id="topic-%s">%s</h2><div class="grid">`,
			html.EscapeString(headingSlug(topic)), html.EscapeString(topicLabel(topic)))
		for _, d := range snap.byTopic[topic] {
			fmt.Fprintf(&body, `<a class="card" href="/wiki/%s"><strong>%s</strong><span>%s</span></a>`,
				pathURL(d.ID), html.EscapeString(d.Title), html.EscapeString(d.Description))
		}
		body.WriteString(`</div>`)
	}

	s.render(w, shell{
		Title: "Jot Knowledge Wiki", NavKey: "index",
		Sidebar: s.sidebar(snap, "", nil), Body: body.String(),
	})
}

func recencySourceLabel(r *Recency) string {
	if r.FromGit() {
		return "from commit history"
	}
	return "from file times"
}

func (s *wikiServer) page(w http.ResponseWriter, req *http.Request) {
	rawID, err := url.PathUnescape(strings.TrimPrefix(req.URL.Path, "/wiki/"))
	if err != nil {
		http.NotFound(w, req)
		return
	}
	id, err := safeID(rawID)
	if err != nil {
		http.NotFound(w, req)
		return
	}
	rel := filepath.ToSlash(filepath.Join("wiki", id+".md"))
	b, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(rel)))
	if err != nil {
		http.NotFound(w, req)
		return
	}
	d, err := parseConcept(rel, b)
	if err != nil {
		http.Error(w, "Invalid wiki page: "+err.Error(), http.StatusInternalServerError)
		return
	}
	d.ID = id

	snap, err := s.site(req.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	now := nowUTC()
	headings := extractHeadings(d)

	var body strings.Builder
	body.WriteString(breadcrumbs(id, d.Title))
	body.WriteString(`<h1>` + html.EscapeString(d.Title) + `</h1>`)
	body.WriteString(s.pageMeta(snap, d, now))
	body.WriteString(renderMarkdown(d, snap.titles))
	body.WriteString(renderConflicts(d, snap.titles))
	body.WriteString(renderSources(d))
	body.WriteString(renderRelated(snap, id))
	body.WriteString(renderBacklinks(snap.graph, snap.titles, id))

	s.render(w, shell{
		Title: d.Title + " · Jot", PageID: id, DocTitle: d.Title, NavKey: "index",
		Sidebar: s.sidebar(snap, id, headings), Body: body.String(),
	})
}

func breadcrumbs(id, title string) string {
	var out strings.Builder
	out.WriteString(`<p class="crumbs"><a href="/">Index</a>`)
	parts := strings.Split(id, "/")
	if len(parts) > 1 {
		fmt.Fprintf(&out, ` / <a href="/tags">%s</a>`, html.EscapeString(topicLabel(parts[0])))
	}
	fmt.Fprintf(&out, ` / %s</p>`, html.EscapeString(title))
	return out.String()
}

func (s *wikiServer) pageMeta(snap *siteSnapshot, d Document, now time.Time) string {
	var out strings.Builder
	out.WriteString(`<p class="meta">`)
	fmt.Fprintf(&out, `<span class="chip">%s</span>`, html.EscapeString(d.Type))
	fmt.Fprintf(&out, `<span class="chip">%s</span>`, html.EscapeString(d.Trust))
	if st := d.EffectiveStatus(); st != StatusStable {
		fmt.Fprintf(&out, `<span class="chip">%s</span>`, html.EscapeString(st))
	}
	if d.IsStale(now) {
		out.WriteString(`<span class="chip warn">stale</span>`)
	}
	for _, tag := range d.Tags {
		fmt.Fprintf(&out, `<a class="chip" href="/tags/%s">#%s</a>`,
			url.PathEscape(strings.ToLower(tag)), html.EscapeString(tag))
	}
	updated := snap.recency.Updated(d.Path)
	if !updated.IsZero() {
		age := humanAge(updated, now)
		class := "chip"
		if now.Sub(updated) > forgottenAfter {
			class = "chip warn"
		}
		fmt.Fprintf(&out, `<span class="%s">edited %s</span>`, class, html.EscapeString(age))
	}
	out.WriteString(`</p>`)
	return out.String()
}

func renderRelated(snap *siteSnapshot, id string) string {
	related := snap.related(id, 5)
	if len(related) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString(`<div class="related"><h2>See also</h2><ul>`)
	for _, n := range related {
		title := snap.titles[n.ID]
		if title == "" {
			title = n.ID
		}
		fmt.Fprintf(&out, `<li><a href="/wiki/%s">%s</a> <span class="meta">%.0f%% similar</span></li>`,
			pathURL(n.ID), html.EscapeString(title), n.Similarity*100)
	}
	out.WriteString(`</ul></div>`)
	return out.String()
}

func renderConflicts(d Document, titles map[string]string) string {
	if len(d.Conflicts) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString(`<div class="related"><h2><span class="badge">Conflict</span>Contradicts</h2><ul>`)
	for _, other := range d.Conflicts {
		id := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(other), "/"), ".md")
		label := titles[id]
		if label == "" {
			label = id
		}
		fmt.Fprintf(&out, `<li><a href="/wiki/%s">%s</a></li>`, pathURL(id), html.EscapeString(label))
	}
	out.WriteString(`</ul></div>`)
	return out.String()
}

func renderSources(d Document) string {
	if len(d.Sources) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString(`<div class="related sources"><h2>Sources</h2><ul>`)
	for _, src := range d.Sources {
		label := src.Title
		if label == "" {
			label = src.Resource
		}
		anchor := ""
		if src.ID != "" {
			anchor = fmt.Sprintf(` id="source-%s"`, html.EscapeString(src.ID))
		}
		if strings.HasPrefix(src.Resource, "http://") || strings.HasPrefix(src.Resource, "https://") {
			fmt.Fprintf(&out, `<li%s><a href="%s" target="_blank" rel="noreferrer">%s</a></li>`,
				anchor, html.EscapeString(src.Resource), html.EscapeString(label))
			continue
		}
		href, _ := wikiHref(src.Resource, d.ID)
		fmt.Fprintf(&out, `<li%s><a href="%s">%s</a></li>`, anchor, html.EscapeString(href), html.EscapeString(label))
	}
	out.WriteString(`</ul></div>`)
	return out.String()
}

func renderBacklinks(graph *LinkGraph, titles map[string]string, id string) string {
	in := graph.Backlinks(id)
	if len(in) == 0 {
		return `<div class="related"><h2>Referenced by</h2><p class="empty">No other page links here yet.</p></div>`
	}
	var out strings.Builder
	fmt.Fprintf(&out, `<div class="related"><h2>Referenced by (%d)</h2><ul>`, len(in))
	for _, from := range in {
		label := titles[from]
		if label == "" {
			label = from
		}
		fmt.Fprintf(&out, `<li><a href="/wiki/%s">%s</a></li>`, pathURL(from), html.EscapeString(label))
	}
	out.WriteString(`</ul></div>`)
	return out.String()
}

// --- search ---------------------------------------------------------------

func searchOptionsFromQuery(q url.Values) (SearchOptions, string) {
	opts := SearchOptions{Limit: 25, MaxChars: 20000, PerPage: 2}
	opts.Type = strings.TrimSpace(q.Get("type"))
	opts.PathPrefix = strings.TrimSpace(q.Get("path"))
	sinceText := strings.TrimSpace(q.Get("since"))
	if since, err := parseSince(sinceText); err == nil {
		opts.Since = since
	}
	if q.Get("raw") == "1" {
		opts.IncludeRaw = true
	}
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 && n <= 50 {
		opts.Limit = n
	}
	return opts, sinceText
}

func (s *wikiServer) search(w http.ResponseWriter, req *http.Request) {
	q := req.URL.Query()
	query := strings.TrimSpace(q.Get("q"))
	snap, err := s.site(req.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	opts, sinceText := searchOptionsFromQuery(q)

	var body strings.Builder
	body.WriteString(`<h1>Search</h1>`)
	body.WriteString(searchFilters(snap, query, opts, sinceText))

	if query == "" {
		body.WriteString(`<p class="empty">Enter a query above, or press <kbd>/</kbd>.</p>`)
		s.render(w, shell{Title: "Search · Jot", NavKey: "index",
			Sidebar: s.sidebar(snap, "", nil), Body: body.String()})
		return
	}

	hits, err := searchVault(s.root, query, opts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	fmt.Fprintf(&body, `<p class="meta">%d results for “%s”</p>`, len(hits), html.EscapeString(query))
	if len(hits) == 0 {
		body.WriteString(`<p class="empty">No matching compiled knowledge.</p>`)
	}
	terms := tokenize(query)
	for _, hit := range hits {
		body.WriteString(`<article class="result">`)
		target := "/wiki/" + pathURL(hit.ID)
		label := hit.Title
		if hit.Raw {
			target = ""
			label = hit.Title + " (raw capture)"
		}
		if target == "" {
			fmt.Fprintf(&body, `<h2>%s</h2>`, html.EscapeString(label))
		} else {
			fmt.Fprintf(&body, `<h2><a href="%s">%s</a></h2>`, target, html.EscapeString(label))
		}
		context := hit.Heading
		if context == "" {
			context = hit.Type
		}
		fmt.Fprintf(&body, `<p class="meta">%s · score %.3f</p>`, html.EscapeString(context), hit.Score)
		fmt.Fprintf(&body, `<p>%s</p>`, highlight(hit.Excerpt, terms))
		body.WriteString(`</article>`)
	}

	s.render(w, shell{Title: "Search · Jot", NavKey: "index",
		Sidebar: s.sidebar(snap, "", nil), Body: body.String()})
}

func searchFilters(snap *siteSnapshot, query string, opts SearchOptions, sinceText string) string {
	types := map[string]bool{}
	for _, d := range snap.docs {
		types[d.Type] = true
	}
	sorted := make([]string, 0, len(types))
	for t := range types {
		sorted = append(sorted, t)
	}
	sort.Strings(sorted)

	var out strings.Builder
	out.WriteString(`<form class="filters" action="/search">`)
	fmt.Fprintf(&out, `<input type="search" name="q" value="%s" placeholder="query" aria-label="Query">`, html.EscapeString(query))
	out.WriteString(`<select name="type" aria-label="Type"><option value="">any type</option>`)
	for _, t := range sorted {
		selected := ""
		if strings.EqualFold(t, opts.Type) {
			selected = " selected"
		}
		fmt.Fprintf(&out, `<option value="%s"%s>%s</option>`, html.EscapeString(t), selected, html.EscapeString(t))
	}
	out.WriteString(`</select>`)
	out.WriteString(`<select name="path" aria-label="Topic"><option value="">any topic</option>`)
	for _, topic := range snap.topics {
		if topic == "" {
			continue
		}
		selected := ""
		if topic == opts.PathPrefix {
			selected = " selected"
		}
		fmt.Fprintf(&out, `<option value="%s"%s>%s</option>`, html.EscapeString(topic), selected, html.EscapeString(topicLabel(topic)))
	}
	out.WriteString(`</select>`)
	fmt.Fprintf(&out, `<input type="text" name="since" value="%s" placeholder="since (2026-01-01)" aria-label="Since">`, html.EscapeString(sinceText))
	checked := ""
	if opts.IncludeRaw {
		checked = " checked"
	}
	fmt.Fprintf(&out, `<label class="meta"><input type="checkbox" name="raw" value="1"%s> raw captures</label>`, checked)
	out.WriteString(`<button type="submit">Filter</button></form>`)
	return out.String()
}

// apiSearch backs the type-ahead dropdown.
func (s *wikiServer) apiSearch(w http.ResponseWriter, req *http.Request) {
	query := strings.TrimSpace(req.URL.Query().Get("q"))
	w.Header().Set("Content-Type", "application/json")
	if query == "" {
		_ = json.NewEncoder(w).Encode(map[string]any{"hits": []any{}})
		return
	}
	limit := 8
	if n, err := strconv.Atoi(req.URL.Query().Get("limit")); err == nil && n > 0 && n <= 20 {
		limit = n
	}
	hits, err := searchVault(s.root, query, SearchOptions{
		Limit: limit, MaxChars: 4000, PerPage: 1, Prefix: true,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type suggestion struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Heading string `json:"heading,omitempty"`
		Type    string `json:"type,omitempty"`
	}
	out := make([]suggestion, 0, len(hits))
	for _, h := range hits {
		if h.Raw {
			continue
		}
		out = append(out, suggestion{ID: h.ID, Title: h.Title, Heading: h.Heading, Type: h.Type})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"hits": out})
}

// --- discovery pages ------------------------------------------------------

func (s *wikiServer) recent(w http.ResponseWriter, req *http.Request) {
	snap, err := s.site(req.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	now := nowUTC()
	var body strings.Builder
	body.WriteString(`<h1>Recent</h1>`)
	fmt.Fprintf(&body, `<p class="meta">Edit times %s.</p>`, html.EscapeString(recencySourceLabel(snap.recency)))

	body.WriteString(`<section class="lane"><h2>Recently updated</h2><ul class="rows">`)
	for _, item := range snap.recency.MostRecent(snap.docs, 25) {
		fmt.Fprintf(&body, `<li><a href="/wiki/%s">%s</a><span class="when">%s</span></li>`,
			pathURL(item.Doc.ID), html.EscapeString(item.Doc.Title), html.EscapeString(humanAge(item.Updated, now)))
	}
	body.WriteString(`</ul></section>`)

	body.WriteString(`<section class="lane"><h2>Longest untouched</h2><ul class="rows">`)
	for _, item := range snap.recency.LeastRecent(snap.docs, 15) {
		fmt.Fprintf(&body, `<li><a href="/wiki/%s">%s</a><span class="when">%s</span></li>`,
			pathURL(item.Doc.ID), html.EscapeString(item.Doc.Title), html.EscapeString(humanAge(item.Updated, now)))
	}
	body.WriteString(`</ul></section>`)

	s.render(w, shell{Title: "Recent · Jot", NavKey: "recent",
		Sidebar: s.sidebar(snap, "", nil), Body: body.String()})
}

func (s *wikiServer) looseEndsPage(w http.ResponseWriter, req *http.Request) {
	snap, err := s.site(req.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	now := nowUTC()
	lanes := s.looseEnds(snap, now)

	total := 0
	for _, lane := range lanes {
		total += len(lane.Items)
	}

	var body strings.Builder
	body.WriteString(`<h1>Loose ends</h1>`)
	if total == 0 {
		body.WriteString(`<p class="empty">Nothing loose. Everything is linked, fresh, and compiled.</p>`)
	} else {
		fmt.Fprintf(&body, `<p class="meta">%d things worth a second look. Nothing here is an error — it is what fell through.</p>`, total)
	}
	for _, lane := range lanes {
		if len(lane.Items) == 0 {
			continue
		}
		fmt.Fprintf(&body, `<section class="lane"><h2>%s <span class="count">%d</span></h2><p class="meta">%s</p><ul class="rows">`,
			html.EscapeString(lane.Title), len(lane.Items), html.EscapeString(lane.Blurb))
		for _, item := range lane.Items {
			if item.Href == "" {
				fmt.Fprintf(&body, `<li><span>%s</span><span class="why">%s</span><span class="when">%s</span></li>`,
					html.EscapeString(item.Title), html.EscapeString(item.Why), html.EscapeString(humanAge(item.When, now)))
				continue
			}
			fmt.Fprintf(&body, `<li><a href="%s">%s</a><span class="why">%s</span><span class="when">%s</span></li>`,
				item.Href, html.EscapeString(item.Title), html.EscapeString(item.Why), html.EscapeString(humanAge(item.When, now)))
		}
		body.WriteString(`</ul></section>`)
	}

	s.render(w, shell{Title: "Loose ends · Jot", NavKey: "loose",
		Sidebar: s.sidebar(snap, "", nil), Body: body.String()})
}

func (s *wikiServer) tags(w http.ResponseWriter, req *http.Request) {
	snap, err := s.site(req.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tag, _ := url.PathUnescape(strings.TrimPrefix(req.URL.Path, "/tags/"))
	tag = strings.ToLower(strings.TrimSpace(tag))

	var body strings.Builder
	if tag == "" || req.URL.Path == "/tags" || req.URL.Path == "/tags/" {
		counts := snap.tagCounts()
		body.WriteString(`<h1>Tags</h1>`)
		if len(counts) == 0 {
			body.WriteString(`<p class="empty">No tags yet. Add a <code>tags:</code> list to a concept's frontmatter.</p>`)
		} else {
			fmt.Fprintf(&body, `<p class="meta">%d tags.</p><div class="tags">`, len(counts))
			for _, c := range counts {
				fmt.Fprintf(&body, `<a href="/tags/%s">%s<b>%d</b></a>`,
					url.PathEscape(c.Tag), html.EscapeString(c.Tag), c.Count)
			}
			body.WriteString(`</div>`)
		}
		// Topic listing doubles as a browse-by-directory index. These jump to
		// the topic's section on the index rather than into a search, which
		// could legitimately return nothing.
		body.WriteString(`<h2>Topics</h2><div class="tags">`)
		for _, topic := range snap.topics {
			fmt.Fprintf(&body, `<a href="/#topic-%s">%s<b>%d</b></a>`,
				url.PathEscape(headingSlug(topic)),
				html.EscapeString(topicLabel(topic)), len(snap.byTopic[topic]))
		}
		body.WriteString(`</div>`)
		s.render(w, shell{Title: "Tags · Jot", NavKey: "tags",
			Sidebar: s.sidebar(snap, "", nil), Body: body.String()})
		return
	}

	ids := snap.tags[tag]
	fmt.Fprintf(&body, `<p class="crumbs"><a href="/tags">Tags</a> / %s</p><h1>#%s</h1>`,
		html.EscapeString(tag), html.EscapeString(tag))
	if len(ids) == 0 {
		body.WriteString(`<p class="empty">Nothing carries this tag.</p>`)
	} else {
		fmt.Fprintf(&body, `<p class="meta">%d pages.</p><div class="grid">`, len(ids))
		for _, id := range ids {
			d := snap.byID[id]
			fmt.Fprintf(&body, `<a class="card" href="/wiki/%s"><strong>%s</strong><span>%s</span></a>`,
				pathURL(id), html.EscapeString(d.Title), html.EscapeString(d.Description))
		}
		body.WriteString(`</div>`)
	}
	s.render(w, shell{Title: "#" + tag + " · Jot", NavKey: "tags",
		Sidebar: s.sidebar(snap, "", nil), Body: body.String()})
}

func (s *wikiServer) graph(w http.ResponseWriter, req *http.Request) {
	snap, err := s.site(req.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var body strings.Builder
	body.WriteString(`<h1>Graph</h1>`)
	fmt.Fprintf(&body, `<p class="meta">%d pages grouped by topic; node size follows link count. Every node is a link.</p>`, len(snap.docs))
	body.WriteString(renderGraph(snap))
	s.render(w, shell{Title: "Graph · Jot", NavKey: "graph", Body: body.String()})
}

func (s *wikiServer) random(w http.ResponseWriter, req *http.Request) {
	snap, err := s.site(req.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(snap.docs) == 0 {
		http.Redirect(w, req, "/", http.StatusSeeOther)
		return
	}
	pick := snap.docs[rand.Intn(len(snap.docs))]
	http.Redirect(w, req, "/wiki/"+pathURL(pick.ID), http.StatusSeeOther)
}

func (s *wikiServer) log(w http.ResponseWriter, req *http.Request) {
	snap, err := s.site(req.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	entries, err := readLog(s.root)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	entries = filterLog(entries, time.Time{}, "", 200)
	var body strings.Builder
	body.WriteString(`<h1>Knowledge Log</h1>`)
	if len(entries) == 0 {
		body.WriteString(`<p class="empty">Nothing logged yet.</p>`)
	}
	for _, e := range entries {
		fmt.Fprintf(&body, `<h2>%s <span class="chip">%s</span></h2><p>%s</p>`,
			html.EscapeString(e.Date), html.EscapeString(e.Kind), html.EscapeString(e.Title))
		if len(e.Details) > 0 {
			body.WriteString(`<ul>`)
			for _, d := range e.Details {
				fmt.Fprintf(&body, `<li>%s</li>`, html.EscapeString(d))
			}
			body.WriteString(`</ul>`)
		}
	}
	s.render(w, shell{Title: "Log · Jot", NavKey: "log",
		Sidebar: s.sidebar(snap, "", nil), Body: body.String()})
}
