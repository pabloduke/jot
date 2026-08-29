package jot

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const wikiCSS = `
:root { color-scheme: light dark; --bg:#fbfaf7; --panel:#fff; --text:#24211d; --muted:#706b64; --line:#dedad2; --accent:#9b4d20; --code:#f1eee8; --mark:#ffe9a8; --markText:#3a2c00; }
@media (prefers-color-scheme:dark) { :root:not([data-theme="light"]) { --bg:#171614; --panel:#211f1c; --text:#ece8e1; --muted:#aaa39a; --line:#3b3833; --accent:#ee9b68; --code:#2b2925; --mark:#5a4a12; --markText:#ffeec2; } }
:root[data-theme="dark"] { --bg:#171614; --panel:#211f1c; --text:#ece8e1; --muted:#aaa39a; --line:#3b3833; --accent:#ee9b68; --code:#2b2925; --mark:#5a4a12; --markText:#ffeec2; }
* { box-sizing:border-box; }
body { margin:0; background:var(--bg); color:var(--text); font:17px/1.65 ui-serif,Georgia,Cambria,"Times New Roman",serif; }
a { color:var(--accent); text-decoration-thickness:.08em; text-underline-offset:.15em; }
header { border-bottom:1px solid var(--line); background:var(--panel); }
.bar { max-width:1120px; margin:auto; padding:1rem 1.5rem; display:flex; align-items:center; gap:1rem; flex-wrap:wrap; }
.brand { color:var(--text); font:700 1.25rem/1 ui-sans-serif,system-ui,sans-serif; text-decoration:none; letter-spacing:-.02em; }
.nav { display:flex; gap:.9rem; font:600 .85rem ui-sans-serif,system-ui,sans-serif; }
.nav a { color:var(--muted); text-decoration:none; }
.nav a:hover { color:var(--accent); }
.search { display:flex; flex:1; max-width:34rem; margin-left:auto; }
.search input { width:100%; padding:.6rem .75rem; border:1px solid var(--line); border-radius:.45rem 0 0 .45rem; background:var(--bg); color:var(--text); font:inherit; }
.search button { border:1px solid var(--line); border-left:0; border-radius:0 .45rem .45rem 0; padding:.6rem .9rem; background:var(--accent); color:#fff; font:600 .9rem ui-sans-serif,system-ui,sans-serif; cursor:pointer; }
#theme { border:1px solid var(--line); background:var(--bg); color:var(--muted); border-radius:.45rem; padding:.45rem .7rem; font:600 .8rem ui-sans-serif,system-ui,sans-serif; cursor:pointer; }
main { max-width:900px; margin:0 auto; padding:2.5rem 1.5rem 5rem; }
.wide { max-width:1120px; }
h1,h2,h3,h4 { font-family:ui-sans-serif,system-ui,sans-serif; line-height:1.2; letter-spacing:-.025em; margin:2rem 0 .8rem; }
h1 { font-size:2.4rem; margin-top:0; } h2 { font-size:1.55rem; border-bottom:1px solid var(--line); padding-bottom:.35rem; }
p,ul,ol,pre,blockquote,table { margin:0 0 1.15rem; }
code { background:var(--code); padding:.12em .35em; border-radius:.25rem; font-size:.9em; }
pre { background:var(--code); padding:1rem; border-radius:.5rem; overflow-x:auto; }
pre code { background:none; padding:0; }
blockquote { border-left:3px solid var(--line); margin-left:0; padding-left:1rem; color:var(--muted); }
hr { border:0; border-top:1px solid var(--line); margin:2rem 0; }
.meta { color:var(--muted); font:.85rem ui-sans-serif,system-ui,sans-serif; }
.empty { color:var(--muted); font-style:italic; }
.grid { display:grid; grid-template-columns:repeat(auto-fill,minmax(15rem,1fr)); gap:.9rem; margin-bottom:2rem; }
.card { display:block; padding:.9rem 1rem; border:1px solid var(--line); border-radius:.5rem; background:var(--panel); text-decoration:none; color:var(--text); }
.card strong { display:block; font-family:ui-sans-serif,system-ui,sans-serif; margin-bottom:.25rem; }
.card span { color:var(--muted); font-size:.85rem; }
.badge { display:inline-block; background:var(--accent); color:#fff; border-radius:.3rem; padding:.05rem .45rem; font:600 .7rem ui-sans-serif,system-ui,sans-serif; text-transform:uppercase; letter-spacing:.04em; margin-right:.4rem; vertical-align:.1em; }
.chip { display:inline-block; border:1px solid var(--line); color:var(--muted); border-radius:1rem; padding:.05rem .6rem; font:600 .72rem ui-sans-serif,system-ui,sans-serif; margin-right:.35rem; }
.tablewrap { overflow-x:auto; }
table { border-collapse:collapse; width:100%; font-size:.92rem; }
th,td { border:1px solid var(--line); padding:.45rem .65rem; text-align:left; }
th { background:var(--code); font-family:ui-sans-serif,system-ui,sans-serif; }
mark { background:var(--mark); color:var(--markText); border-radius:.2rem; padding:0 .15em; }
.result { border-bottom:1px solid var(--line); padding-bottom:.5rem; }
.result h2 { border:0; margin-bottom:.2rem; font-size:1.2rem; }
.backlinks { margin-top:3rem; border-top:1px solid var(--line); padding-top:1rem; }
.backlinks h2 { border:0; font-size:1.1rem; }
.backlinks ul { margin:0; padding-left:1.1rem; }
.sources { font-size:.9rem; }
footer { max-width:1120px; margin:auto; padding:2rem 1.5rem 3rem; color:var(--muted); font:.8rem ui-sans-serif,system-ui,sans-serif; border-top:1px solid var(--line); }
`

const wikiJS = `(function(){
  var root=document.documentElement, key="jot-theme";
  try { var saved=localStorage.getItem(key); if(saved){ root.setAttribute("data-theme",saved); } } catch(e){}
  var btn=document.getElementById("theme");
  if(!btn) return;
  btn.addEventListener("click",function(){
    var explicit=root.getAttribute("data-theme");
    var dark = explicit ? explicit==="dark"
      : window.matchMedia("(prefers-color-scheme: dark)").matches;
    var next = dark ? "light" : "dark";
    root.setAttribute("data-theme",next);
    try { localStorage.setItem(key,next); } catch(e){}
  });
})();`

//go:embed favicon.svg
var faviconSVG []byte

type wikiServer struct {
	root string
}

func (r *runner) serve(args []string) error {
	fs := newFlags("serve", r.errOut)
	// Bind to every interface by default: this is a personal wiki meant to be
	// read from a phone or laptop on the same trusted network.
	bind := fs.String("bind", "0.0.0.0", "interface to bind")
	port := fs.Int("port", 8787, "TCP port")
	vaultFlag := fs.String("vault", "", "vault path (defaults to configured vault)")
	watch := fs.Duration("watch", 0, "re-synchronize with GitHub on this interval, e.g. 5m")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *port < 1 || *port > 65535 {
		return codedf(ExitUsage, "port must be between 1 and 65535")
	}
	root := *vaultFlag
	if root == "" {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		root = cfg.Vault
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	root = abs
	if _, err := os.Stat(filepath.Join(root, "wiki")); err != nil {
		return fmt.Errorf("vault has no wiki directory: %w", err)
	}
	if err := syncLocked(r.ctx, root); err != nil {
		return err
	}

	addr := net.JoinHostPort(*bind, strconv.Itoa(*port))
	if *bind == "0.0.0.0" || *bind == "::" || *bind == "" {
		fmt.Fprintf(r.errOut, "WARNING: the compiled personal wiki is visible to every device that can reach port %d; raw captures are not served.\n", *port)
	}
	fmt.Fprintf(r.out, "Serving Jot wiki from %s on http://%s\n", root, addr)

	server := &http.Server{
		Addr:              addr,
		Handler:           newWikiHandler(root),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	if *watch > 0 {
		go r.watchLoop(root, *watch)
	}

	go func() {
		<-r.ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()

	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// syncLocked takes the vault lock for the duration of one synchronization.
func syncLocked(ctx context.Context, root string) error {
	lock, err := lockVault(root)
	if err != nil {
		return err
	}
	defer lock.Close()
	return syncBefore(ctx, root, false)
}

// watchLoop keeps a long-running server from drifting away from the remote.
func (r *runner) watchLoop(root string, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			if err := syncLocked(r.ctx, root); err != nil {
				fmt.Fprintf(r.errOut, "watch: %v\n", err)
			}
		}
	}
}

func newWikiHandler(root string) http.Handler {
	w := &wikiServer{root: root}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", w.home)
	mux.HandleFunc("GET /wiki/", w.page)
	mux.HandleFunc("GET /search", w.search)
	mux.HandleFunc("GET /log", w.log)
	mux.HandleFunc("GET /assets/wiki.css", w.css)
	mux.HandleFunc("GET /assets/wiki.js", w.js)
	mux.HandleFunc("GET /assets/favicon.svg", w.favicon)
	mux.HandleFunc("GET /healthz", w.health)
	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; script-src 'self'; img-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, req)
	})
}

func (s *wikiServer) home(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path != "/" {
		http.NotFound(w, req)
		return
	}
	docs, err := listConcepts(s.root, false)
	if err != nil {
		http.Error(w, "Cannot read wiki: "+err.Error(), http.StatusInternalServerError)
		return
	}
	groups := map[string][]Document{}
	for _, d := range docs {
		groups[topicOf(d.ID)] = append(groups[topicOf(d.ID)], d)
	}
	topics := make([]string, 0, len(groups))
	for topic := range groups {
		topics = append(topics, topic)
	}
	sort.Strings(topics)

	var body strings.Builder
	body.WriteString(`<main class="wide"><h1>Knowledge Wiki</h1>`)
	fmt.Fprintf(&body, `<p class="meta">%d compiled concepts from your local Jot vault.</p>`, len(docs))
	if len(docs) == 0 {
		body.WriteString(`<p class="empty">No compiled wiki pages yet. Add a capture and let an AI harness compile it.</p>`)
	}
	for _, topic := range topics {
		fmt.Fprintf(&body, `<h2 class="topic">%s</h2><div class="grid">`, html.EscapeString(topicLabel(topic)))
		for _, d := range groups[topic] {
			fmt.Fprintf(&body, `<a class="card" href="/wiki/%s"><strong>%s</strong><span>%s</span></a>`,
				pathURL(d.ID), html.EscapeString(d.Title), html.EscapeString(d.Description))
		}
		body.WriteString(`</div>`)
	}
	body.WriteString(`</main>`)
	writeHTML(w, "Jot Knowledge Wiki", body.String())
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

	docs, _, err := loadConcepts(s.root, true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	graph := buildLinkGraph(s.root, docs)
	titles := map[string]string{}
	for _, other := range docs {
		titles[other.ID] = other.Title
	}

	var meta strings.Builder
	meta.WriteString(`<p class="meta">`)
	fmt.Fprintf(&meta, `<span class="chip">%s</span>`, html.EscapeString(d.Type))
	fmt.Fprintf(&meta, `<span class="chip">%s</span>`, html.EscapeString(d.Trust))
	if st := d.EffectiveStatus(); st != StatusStable {
		fmt.Fprintf(&meta, `<span class="chip">%s</span>`, html.EscapeString(st))
	}
	if d.IsStale(nowUTC()) {
		meta.WriteString(`<span class="chip">stale</span>`)
	}
	if ts := d.Timestamp(); ts != "" {
		fmt.Fprintf(&meta, ` Updated %s`, html.EscapeString(ts))
	}
	meta.WriteString(`</p>`)

	var body strings.Builder
	body.WriteString(`<main><h1>` + html.EscapeString(d.Title) + `</h1>`)
	body.WriteString(meta.String())
	body.WriteString(renderMarkdown(d, titles))
	body.WriteString(renderConflicts(d, titles))
	body.WriteString(renderSources(d))
	body.WriteString(renderBacklinks(graph, titles, id))
	body.WriteString(`</main>`)
	writeHTML(w, d.Title+" · Jot", body.String())
}

// renderConflicts surfaces recorded contradictions as a first-class block
// rather than leaving them as prose an agent has to notice.
func renderConflicts(d Document, titles map[string]string) string {
	if len(d.Conflicts) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString(`<div class="backlinks"><h2><span class="badge">Conflict</span>Contradicts</h2><ul>`)
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
	out.WriteString(`<div class="backlinks sources"><h2>Sources</h2><ul>`)
	for _, src := range d.Sources {
		label := src.Title
		if label == "" {
			label = src.Resource
		}
		if strings.HasPrefix(src.Resource, "http://") || strings.HasPrefix(src.Resource, "https://") {
			fmt.Fprintf(&out, `<li><a href="%s" target="_blank" rel="noreferrer">%s</a></li>`,
				html.EscapeString(src.Resource), html.EscapeString(label))
			continue
		}
		href, _ := wikiHref(src.Resource, d.ID)
		fmt.Fprintf(&out, `<li><a href="%s">%s</a></li>`, html.EscapeString(href), html.EscapeString(label))
	}
	out.WriteString(`</ul></div>`)
	return out.String()
}

func renderBacklinks(graph *LinkGraph, titles map[string]string, id string) string {
	in := graph.Backlinks(id)
	if len(in) == 0 {
		return `<div class="backlinks"><h2>Referenced by</h2><p class="empty">No other page links here yet.</p></div>`
	}
	var out strings.Builder
	fmt.Fprintf(&out, `<div class="backlinks"><h2>Referenced by (%d)</h2><ul>`, len(in))
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

func (s *wikiServer) search(w http.ResponseWriter, req *http.Request) {
	query := strings.TrimSpace(req.URL.Query().Get("q"))
	var body strings.Builder
	body.WriteString(`<main><h1>Search</h1>`)
	if query == "" {
		body.WriteString(`<p class="empty">Enter a query above.</p></main>`)
		writeHTML(w, "Search · Jot", body.String())
		return
	}
	hits, err := searchVault(s.root, query, SearchOptions{Limit: 25, MaxChars: 20000, PerPage: 2})
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
		fmt.Fprintf(&body, `<h2><a href="/wiki/%s">%s</a></h2>`, pathURL(hit.ID), html.EscapeString(hit.Title))
		context := hit.Heading
		if context == "" {
			context = hit.Type
		}
		fmt.Fprintf(&body, `<p class="meta">%s · score %.3f</p>`, html.EscapeString(context), hit.Score)
		fmt.Fprintf(&body, `<p>%s</p>`, highlight(hit.Excerpt, terms))
		body.WriteString(`</article>`)
	}
	body.WriteString(`</main>`)
	writeHTML(w, "Search · Jot", body.String())
}

func (s *wikiServer) log(w http.ResponseWriter, _ *http.Request) {
	entries, err := readLog(s.root)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	entries = filterLog(entries, time.Time{}, "", 200)
	var body strings.Builder
	body.WriteString(`<main><h1>Knowledge Log</h1>`)
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
	body.WriteString(`</main>`)
	writeHTML(w, "Log · Jot", body.String())
}

func (s *wikiServer) css(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = io.WriteString(w, wikiCSS)
}

func (s *wikiServer) js(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = io.WriteString(w, wikiJS)
}

func (s *wikiServer) favicon(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(faviconSVG)
}

func (s *wikiServer) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "wiki": filepath.Join(s.root, "wiki"), "version": Version()})
}

func writeHTML(w http.ResponseWriter, title, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8">`+
		`<meta name="viewport" content="width=device-width,initial-scale=1">`+
		`<meta name="robots" content="noindex,nofollow"><meta name="theme-color" content="#1f1c18">`+
		`<title>%s</title><link rel="icon" type="image/svg+xml" href="/assets/favicon.svg">`+
		`<link rel="stylesheet" href="/assets/wiki.css"></head><body><header><div class="bar">`+
		`<a class="brand" href="/">jot</a>`+
		`<nav class="nav"><a href="/">Index</a><a href="/log">Log</a></nav>`+
		`<form class="search" action="/search"><input type="search" name="q" aria-label="Search wiki" placeholder="Search your knowledge…"><button>Search</button></form>`+
		`<button id="theme" type="button">Theme</button>`+
		`</div></header>%s<footer>Read-only view of the local compiled wiki. Raw captures are not exposed.</footer>`+
		`<script src="/assets/wiki.js"></script></body></html>`, html.EscapeString(title), body)
}

func pathURL(id string) string {
	parts := strings.Split(filepath.ToSlash(id), "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

// highlight escapes text and wraps query terms in <mark>.
func highlight(text string, terms []string) string {
	escaped := html.EscapeString(text)
	if len(terms) == 0 {
		return escaped
	}
	unique := dedupe(terms)
	quoted := make([]string, 0, len(unique))
	for _, t := range unique {
		if len(t) < 2 {
			continue
		}
		quoted = append(quoted, regexp.QuoteMeta(html.EscapeString(t)))
	}
	if len(quoted) == 0 {
		return escaped
	}
	re, err := regexp.Compile(`(?i)\b(` + strings.Join(quoted, "|") + `)`)
	if err != nil {
		return escaped
	}
	return re.ReplaceAllString(escaped, "<mark>$1</mark>")
}

var inlinePattern = regexp.MustCompile(
	"`([^`]+)`" + // code
		`|\[\[([^\]\|\n]+)(?:\|([^\]\n]*))?\]\]` + // wiki link
		`|\[\^([^\]\s]+)\]` + // footnote
		`|\[([^]\n]+)\]\(([^)[:space:]]+)\)` + // markdown link
		`|\*\*([^*\n]+)\*\*` + // bold
		`|\*([^*\n]+)\*` + // italic
		`|_([^_\n]+)_`) // italic

// renderInline formats one line of Markdown. titles maps concept ids to page
// titles so that [[wiki links]] can render a human label.
func renderInline(text, currentID string, titles map[string]string) string {
	var out strings.Builder
	last := 0
	for _, loc := range inlinePattern.FindAllStringSubmatchIndex(text, -1) {
		out.WriteString(html.EscapeString(text[last:loc[0]]))
		switch {
		case loc[2] >= 0: // code
			out.WriteString("<code>" + html.EscapeString(text[loc[2]:loc[3]]) + "</code>")
		case loc[4] >= 0: // wiki link
			target := text[loc[4]:loc[5]]
			href, external := wikiHref(target, currentID)
			label := target
			if loc[6] >= 0 && text[loc[6]:loc[7]] != "" {
				label = text[loc[6]:loc[7]]
			} else if !external {
				// Prefer the target page's own title over the raw slug.
				if title, ok := titles[conceptIDFromHref(href)]; ok && title != "" {
					label = title
				}
			}
			fmt.Fprintf(&out, `<a href="%s">%s</a>`, html.EscapeString(href), html.EscapeString(label))
		case loc[8] >= 0: // footnote
			id := text[loc[8]:loc[9]]
			fmt.Fprintf(&out, `<sup><a href="#source-%s">%s</a></sup>`, html.EscapeString(id), html.EscapeString(id))
		case loc[10] >= 0: // markdown link
			label := html.EscapeString(text[loc[10]:loc[11]])
			href, external := wikiHref(text[loc[12]:loc[13]], currentID)
			if external {
				fmt.Fprintf(&out, `<a href="%s" target="_blank" rel="noreferrer">%s</a>`, html.EscapeString(href), label)
			} else {
				fmt.Fprintf(&out, `<a href="%s">%s</a>`, html.EscapeString(href), label)
			}
		case loc[14] >= 0: // bold
			out.WriteString("<strong>" + html.EscapeString(text[loc[14]:loc[15]]) + "</strong>")
		case loc[16] >= 0: // italic with *
			out.WriteString("<em>" + html.EscapeString(text[loc[16]:loc[17]]) + "</em>")
		case loc[18] >= 0: // italic with _
			out.WriteString("<em>" + html.EscapeString(text[loc[18]:loc[19]]) + "</em>")
		}
		last = loc[1]
	}
	out.WriteString(html.EscapeString(text[last:]))
	return out.String()
}

// conceptIDFromHref recovers the concept id from a rendered wiki href.
func conceptIDFromHref(href string) string {
	id := strings.TrimPrefix(href, "/wiki/")
	if before, _, ok := strings.Cut(id, "#"); ok {
		id = before
	}
	unescaped, err := url.PathUnescape(id)
	if err != nil {
		return id
	}
	return unescaped
}

func wikiHref(target, currentID string) (string, bool) {
	if u, err := url.Parse(target); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		return target, true
	}
	fragment := ""
	if before, after, ok := strings.Cut(target, "#"); ok {
		target, fragment = before, "#"+url.PathEscape(after)
	}
	target = strings.TrimSuffix(target, ".md")
	var id string
	if strings.HasPrefix(target, "/") {
		id = strings.TrimPrefix(target, "/")
	} else {
		id = filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(currentID), filepath.FromSlash(target))))
	}
	if safe, err := safeID(id); err == nil {
		return "/wiki/" + pathURL(safe) + fragment, false
	}
	return "#", false
}

var tableDividerPattern = regexp.MustCompile(`^\|?[\s:|-]+\|[\s:|-]*$`)

func isTableRow(line string) bool {
	trim := strings.TrimSpace(line)
	return strings.HasPrefix(trim, "|") && strings.Count(trim, "|") >= 2
}

func splitTableRow(line string) []string {
	trim := strings.TrimSpace(line)
	trim = strings.TrimPrefix(trim, "|")
	trim = strings.TrimSuffix(trim, "|")
	cells := strings.Split(trim, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

var orderedItemPattern = regexp.MustCompile(`^(\d+)[.)]\s+(.*)$`)

// leadingIndent measures a list item's indent, counting a tab as four columns.
func leadingIndent(line string) int {
	n := 0
	for _, r := range line {
		switch r {
		case ' ':
			n++
		case '\t':
			n += 4
		default:
			return n
		}
	}
	return n
}

// renderMarkdown converts a concept body to HTML. It deliberately implements a
// small, predictable subset rather than pulling in a full CommonMark engine.
func renderMarkdown(d Document, titles map[string]string) string {
	lines := strings.Split(strings.ReplaceAll(d.Body, "\r\n", "\n"), "\n")
	var out strings.Builder
	var paragraph []string
	inCode, authoritative := false, false

	// Lists are tracked as a stack of (tag, indent) so that indented items
	// nest instead of flattening.
	type listLevel struct {
		tag    string
		indent int
	}
	var lists []listLevel

	badge := func() {
		if authoritative {
			out.WriteString(`<span class="badge">Authoritative</span>`)
			authoritative = false
		}
	}
	flushParagraph := func() {
		if len(paragraph) == 0 {
			return
		}
		badge()
		out.WriteString("<p>" + renderInline(strings.Join(paragraph, " "), d.ID, titles) + "</p>")
		paragraph = nil
	}
	closeList := func() {
		for len(lists) > 0 {
			out.WriteString("</" + lists[len(lists)-1].tag + ">")
			lists = lists[:len(lists)-1]
		}
	}
	// openItem reconciles the list stack with one item's indent and marker,
	// then emits the opening <li>.
	openItem := func(tag string, indent int) {
		for len(lists) > 0 {
			top := lists[len(lists)-1]
			if indent > top.indent {
				break
			}
			if indent == top.indent && tag == top.tag {
				break
			}
			out.WriteString("</" + top.tag + ">")
			lists = lists[:len(lists)-1]
		}
		if len(lists) == 0 || indent > lists[len(lists)-1].indent {
			out.WriteString("<" + tag + ">")
			lists = append(lists, listLevel{tag: tag, indent: indent})
		}
		out.WriteString("<li>")
	}

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trim := strings.TrimSpace(line)

		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			flushParagraph()
			closeList()
			if !inCode {
				badge()
				out.WriteString("<pre><code>")
			} else {
				out.WriteString("</code></pre>")
			}
			inCode = !inCode
			continue
		}
		if inCode {
			out.WriteString(html.EscapeString(line) + "\n")
			continue
		}
		if trim == authorityMarker {
			flushParagraph()
			closeList()
			authoritative = true
			continue
		}
		if trim == "" {
			flushParagraph()
			closeList()
			continue
		}
		if trim == "---" || trim == "***" || trim == "___" {
			flushParagraph()
			closeList()
			out.WriteString("<hr>")
			continue
		}
		// Tables: a header row followed by a divider row.
		if isTableRow(line) && i+1 < len(lines) && tableDividerPattern.MatchString(strings.TrimSpace(lines[i+1])) {
			flushParagraph()
			closeList()
			badge()
			out.WriteString(`<div class="tablewrap"><table><thead><tr>`)
			for _, cell := range splitTableRow(line) {
				out.WriteString("<th>" + renderInline(cell, d.ID, titles) + "</th>")
			}
			out.WriteString("</tr></thead><tbody>")
			i++
			for i+1 < len(lines) && isTableRow(lines[i+1]) {
				i++
				out.WriteString("<tr>")
				for _, cell := range splitTableRow(lines[i]) {
					out.WriteString("<td>" + renderInline(cell, d.ID, titles) + "</td>")
				}
				out.WriteString("</tr>")
			}
			out.WriteString("</tbody></table></div>")
			continue
		}
		if strings.HasPrefix(trim, "#") {
			flushParagraph()
			closeList()
			level := len(trim) - len(strings.TrimLeft(trim, "#"))
			if level > 6 {
				level = 6
			}
			text := strings.TrimSpace(trim[level:])
			if level == 1 && strings.EqualFold(text, d.Title) {
				continue
			}
			badge()
			fmt.Fprintf(&out, "<h%d>%s</h%d>", level, renderInline(text, d.ID, titles), level)
			continue
		}
		if strings.HasPrefix(trim, "- ") || strings.HasPrefix(trim, "* ") || strings.HasPrefix(trim, "+ ") {
			flushParagraph()
			openItem("ul", leadingIndent(line))
			badge()
			out.WriteString(renderInline(strings.TrimSpace(trim[2:]), d.ID, titles) + "</li>")
			continue
		}
		if m := orderedItemPattern.FindStringSubmatch(trim); m != nil {
			flushParagraph()
			openItem("ol", leadingIndent(line))
			badge()
			out.WriteString(renderInline(m[2], d.ID, titles) + "</li>")
			continue
		}
		if strings.HasPrefix(trim, "> ") {
			flushParagraph()
			closeList()
			badge()
			out.WriteString("<blockquote>" + renderInline(strings.TrimSpace(trim[2:]), d.ID, titles) + "</blockquote>")
			continue
		}
		paragraph = append(paragraph, trim)
	}
	flushParagraph()
	closeList()
	if inCode {
		out.WriteString("</code></pre>")
	}
	return out.String()
}
