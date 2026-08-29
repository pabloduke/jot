package jot

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const usage = `jot — a Git-native personal knowledge system

Capture
  jot -m "note"                         Capture a quick message
  jot add [-m TEXT|--stdin|FILE]        Capture text, a file, or a URL
  jot pending [--json]                  List captures awaiting compilation
  jot captures [--status S] [--json]    List captures in any state
  jot reopen ID                         Return a compiled capture to pending

Recall
  jot context [flags] QUERY             Return ranked knowledge passages
  jot get [--json] ID                   Read a concept or capture
  jot index [--json]                    List compiled concepts
  jot backlinks [--json] ID             Show pages linking to a concept
  jot log [--json] [--since D]          Read the knowledge log
  jot open ID | jot edit ID             Open a concept in $PAGER / $EDITOR

Write
  jot apply [--dry-run] --stdin         Apply an atomic agent transaction
  jot publish [flags]                   Publish direct Markdown edits
  jot promote [flags] --stdin           Turn an answer into a permanent page

Maintain
  jot lint [--json] [--fix]             Validate the vault
  jot maintain [--json] [--drain N]     Deterministic maintenance queue
  jot status [--json]                   Show local and capture state
  jot sync [--continue]                 Synchronize with GitHub

Share
  jot serve [flags]                     Serve the compiled wiki over HTTP
  jot export DIR                        Write a portable OKF bundle
  jot import --prefix P DIR             Import an external OKF bundle

Other
  jot path                              Print the vault path
  jot guide                             Print the agent maintenance contract
  jot completion bash|zsh|fish          Print a shell completion script
  jot version                           Print the build version

Exit codes: 0 ok, 1 error, 2 lint findings, 3 not initialized, 4 conflict.
`

type runner struct {
	ctx    context.Context
	out    io.Writer
	errOut io.Writer
}

var commandNames = []string{
	"add", "apply", "backlinks", "captures", "completion", "context", "edit",
	"export", "get", "guide", "help", "import", "index", "init", "lint", "log",
	"maintain", "open", "path", "pending", "promote", "publish", "reopen",
	"serve", "status", "sync", "version",
}

// Run dispatches one CLI invocation. Errors may carry an exit code; see
// ExitCode.
func Run(ctx context.Context, args []string, out, errOut io.Writer) error {
	r := &runner{ctx: ctx, out: out, errOut: errOut}
	if len(args) == 0 {
		fmt.Fprint(out, usage)
		return nil
	}
	if args[0] == "-m" || args[0] == "--message" {
		args = append([]string{"add"}, args...)
	}
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(out, usage)
		return nil
	case "version", "--version", "-v":
		fmt.Fprintln(out, "jot "+Version())
		return nil
	case "init":
		return r.init(args[1:])
	case "add":
		return r.withVault(false, false, func(root string) error { return r.add(root, args[1:]) })
	case "pending":
		return r.withVault(false, false, func(root string) error { return r.pending(root, args[1:]) })
	case "captures":
		return r.withVault(false, false, func(root string) error { return r.captures(root, args[1:]) })
	case "reopen":
		return r.withVault(false, false, func(root string) error { return r.reopen(root, args[1:]) })
	case "context":
		return r.withVault(false, false, func(root string) error { return r.context(root, args[1:]) })
	case "get":
		return r.withVault(false, false, func(root string) error { return r.get(root, args[1:]) })
	case "index":
		return r.withVault(false, false, func(root string) error { return r.index(root, args[1:]) })
	case "backlinks":
		return r.withVault(false, false, func(root string) error { return r.backlinks(root, args[1:]) })
	case "log":
		return r.withVault(false, false, func(root string) error { return r.log(root, args[1:]) })
	case "open", "edit":
		return r.withVault(false, false, func(root string) error { return r.openIn(root, args[0], args[1:]) })
	case "apply":
		return r.withVault(false, false, func(root string) error { return r.apply(root, args[1:]) })
	case "publish":
		return r.withVault(true, true, func(root string) error { return r.publish(root, args[1:]) })
	case "promote":
		return r.withVault(false, false, func(root string) error { return r.promote(root, args[1:]) })
	case "maintain":
		return r.withVault(false, false, func(root string) error { return r.maintain(root, args[1:]) })
	case "export":
		return r.withVault(false, false, func(root string) error { return r.export(root, args[1:]) })
	case "import":
		return r.withVault(false, false, func(root string) error { return r.importBundle(root, args[1:]) })
	case "sync":
		return r.sync(args[1:])
	case "status":
		return r.withVault(false, false, func(root string) error { return r.status(root, args[1:]) })
	case "lint":
		return r.withVault(false, false, func(root string) error { return r.lint(root, args[1:]) })
	case "serve":
		return r.serve(args[1:])
	case "completion":
		return r.completion(args[1:])
	case "path":
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		fmt.Fprintln(r.out, cfg.Vault)
		return nil
	case "guide":
		fmt.Fprint(r.out, jotGuide)
		return nil
	default:
		return codedf(ExitUsage, "unknown command %q\n\n%s", args[0], usage)
	}
}

func (r *runner) withVault(skipBefore, allowAuthority bool, fn func(string) error) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if err := scaffoldVault(cfg.Vault); err != nil {
		return err
	}
	lock, err := lockVault(cfg.Vault)
	if err != nil {
		return err
	}
	defer lock.Close()
	if !skipBefore {
		if err := syncBefore(r.ctx, cfg.Vault, allowAuthority); err != nil {
			return err
		}
	}
	return fn(cfg.Vault)
}

func jsonOut(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func newFlags(name string, errOut io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(errOut)
	return fs
}

func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return coded(ExitUsage, err)
	}
	return nil
}

func (r *runner) init(args []string) error {
	fs := newFlags("init", r.errOut)
	vaultFlag := fs.String("vault", "", "local vault path")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return codedf(ExitUsage, "usage: jot init OWNER/REPO")
	}
	remote := fs.Arg(0)
	if !strings.Contains(remote, "/") || strings.ContainsAny(remote, " \t\n") {
		return codedf(ExitUsage, "GitHub repository must be OWNER/REPO")
	}
	root := *vaultFlag
	if root == "" {
		var err error
		root, err = defaultVaultPath()
		if err != nil {
			return err
		}
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if isGitRepo(r.ctx, root) {
		// Re-running init repairs missing scaffold files without replacing content.
	} else {
		if entries, readErr := os.ReadDir(root); readErr == nil && len(entries) > 0 {
			return fmt.Errorf("vault directory %s exists and is not an empty Git repository", root)
		}
		if _, err := command(r.ctx, ".", "gh", "auth", "status"); err != nil {
			return fmt.Errorf("GitHub CLI is not authenticated: %w", err)
		}
		if _, err := command(r.ctx, ".", "gh", "repo", "view", remote); err != nil {
			if _, createErr := command(r.ctx, ".", "gh", "repo", "create", remote, "--private"); createErr != nil {
				return createErr
			}
		}
		if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
			return err
		}
		if _, err := command(r.ctx, filepath.Dir(root), "gh", "repo", "clone", remote, root); err != nil {
			return err
		}
	}
	if err := scaffoldVault(root); err != nil {
		return err
	}
	if err := saveConfig(Config{Vault: root, Remote: remote}); err != nil {
		return err
	}
	lock, err := lockVault(root)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syncAfter(r.ctx, root, "jot: initialize knowledge vault", false); err != nil {
		return err
	}
	fmt.Fprintf(r.out, "Initialized jot at %s (private GitHub repository %s)\n", root, remote)
	return nil
}

func (r *runner) add(root string, args []string) error {
	fs := newFlags("add", r.errOut)
	message := fs.String("m", "", "message text")
	fs.StringVar(message, "message", "", "message text")
	stdin := fs.Bool("stdin", false, "read content from stdin")
	sourceURL := fs.String("url", "", "source URL")
	title := fs.String("title", "", "capture title")
	tagsText := fs.String("tags", "", "comma-separated tags")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	provided := 0
	if *message != "" {
		provided++
	}
	if *stdin {
		provided++
	}
	if fs.NArg() > 0 {
		provided++
	}
	if provided > 1 || fs.NArg() > 1 {
		return codedf(ExitUsage, "choose exactly one of -m, --stdin, or a file path")
	}
	content, kind := *message, "message"
	if *stdin {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		content, kind = string(b), "stdin"
	} else if fs.NArg() == 1 {
		b, err := os.ReadFile(fs.Arg(0))
		if err != nil {
			return err
		}
		content, kind = string(b), "file"
		if *title == "" {
			*title = filepath.Base(fs.Arg(0))
		}
	}
	if *sourceURL != "" {
		kind = "url"
	}
	var tags []string
	if strings.TrimSpace(*tagsText) != "" {
		tags = strings.Split(*tagsText, ",")
	}
	c, err := addCapture(root, *title, kind, *sourceURL, content, tags)
	if err != nil {
		return err
	}
	if err := syncAfter(r.ctx, root, "jot: capture "+c.ID, false); err != nil {
		return err
	}
	if *asJSON {
		return jsonOut(r.out, c)
	}
	fmt.Fprintf(r.out, "Captured %s (%s)\n", c.ID, c.Path)
	return nil
}

func (r *runner) pending(root string, args []string) error {
	fs := newFlags("pending", r.errOut)
	asJSON := fs.Bool("json", false, "JSON output")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	items, err := pendingCaptures(root)
	if err != nil {
		return err
	}
	if *asJSON {
		return jsonOut(r.out, map[string]any{"revision": currentRevision(r.ctx, root), "captures": items})
	}
	if len(items) == 0 {
		fmt.Fprintln(r.out, "No pending captures.")
		return nil
	}
	for _, item := range items {
		fmt.Fprintf(r.out, "%s  %s  %s\n", item.ID, item.SourceKind, item.Title)
	}
	return nil
}

func (r *runner) captures(root string, args []string) error {
	fs := newFlags("captures", r.errOut)
	status := fs.String("status", "all", "pending, compiled, no-material, or all")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	items, err := listCaptures(root, *status)
	if err != nil {
		return err
	}
	if *asJSON {
		return jsonOut(r.out, map[string]any{"status": *status, "captures": items})
	}
	if len(items) == 0 {
		fmt.Fprintf(r.out, "No captures with status %q.\n", *status)
		return nil
	}
	for _, item := range items {
		fmt.Fprintf(r.out, "%s  %-12s %-8s %s\n", item.ID, item.Status, item.SourceKind, item.Title)
	}
	return nil
}

func (r *runner) reopen(root string, args []string) error {
	fs := newFlags("reopen", r.errOut)
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return codedf(ExitUsage, "usage: jot reopen CAPTURE_ID")
	}
	if err := reopenCapture(root, fs.Arg(0)); err != nil {
		return err
	}
	if err := syncAfter(r.ctx, root, "jot: reopen capture "+fs.Arg(0), false); err != nil {
		return err
	}
	fmt.Fprintf(r.out, "Capture %s is pending again.\n", fs.Arg(0))
	return nil
}

func (r *runner) context(root string, args []string) error {
	fs := newFlags("context", r.errOut)
	limit := fs.Int("limit", 8, "maximum passages")
	maxChars := fs.Int("max-chars", 6000, "maximum excerpt characters (0 for no budget)")
	perPage := fs.Int("per-page", 2, "maximum passages from one page")
	typeFilter := fs.String("type", "", "only concepts of this OKF type")
	pathFilter := fs.String("path", "", "only concepts under this id prefix")
	sinceText := fs.String("since", "", "only concepts generated after this date or duration")
	includeRaw := fs.Bool("include-raw", false, "also search uncompiled raw captures")
	full := fs.Bool("full", false, "return whole pages instead of excerpts")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	query := strings.Join(fs.Args(), " ")
	if strings.TrimSpace(query) == "" {
		return codedf(ExitUsage, "context query is required")
	}
	since, err := parseSince(*sinceText)
	if err != nil {
		return coded(ExitUsage, err)
	}
	opts := SearchOptions{
		Limit: *limit, MaxChars: *maxChars, PerPage: *perPage, Type: *typeFilter,
		PathPrefix: *pathFilter, Since: since, IncludeRaw: *includeRaw, Full: *full,
	}
	hits, err := searchVault(root, query, opts)
	if err != nil {
		return err
	}
	queryID := recordQuery(root, query, hits)
	result := map[string]any{
		"query": query, "query_id": queryID,
		"revision": currentRevision(r.ctx, root), "hits": hits,
	}
	if *asJSON {
		return jsonOut(r.out, result)
	}
	fmt.Fprintf(r.out, "Revision: %s\n", currentRevision(r.ctx, root))
	if len(hits) == 0 {
		fmt.Fprintln(r.out, "No matching compiled knowledge.")
		return nil
	}
	for _, hit := range hits {
		origin := hit.Path
		if hit.Raw {
			origin += " (raw capture)"
		}
		fmt.Fprintf(r.out, "\n%s [%s] (%.4f) %s\n%s\n", hit.Title, origin, hit.Score, hit.Trust, hit.Excerpt)
	}
	return nil
}

func (r *runner) get(root string, args []string) error {
	fs := newFlags("get", r.errOut)
	asJSON := fs.Bool("json", false, "JSON output")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return codedf(ExitUsage, "usage: jot get ID")
	}
	path, err := resolveGetPath(root, fs.Arg(0))
	if err != nil {
		return err
	}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return err
	}
	if *asJSON {
		return jsonOut(r.out, map[string]any{"id": fs.Arg(0), "path": path, "content": string(b)})
	}
	_, err = r.out.Write(b)
	return err
}

func resolveGetPath(root, id string) (string, error) {
	m, err := loadManifest(root)
	if err != nil {
		return "", err
	}
	if rec, ok := m.Captures[id]; ok {
		return rec.Path, nil
	}
	safe, err := safeID(id)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Join("wiki", safe+".md")), nil
}

func (r *runner) index(root string, args []string) error {
	fs := newFlags("index", r.errOut)
	asJSON := fs.Bool("json", false, "JSON output")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	docs, err := listConcepts(root, false)
	if err != nil {
		return err
	}
	if *asJSON {
		return jsonOut(r.out, map[string]any{"revision": currentRevision(r.ctx, root), "concepts": docs})
	}
	for _, d := range docs {
		fmt.Fprintf(r.out, "%s  %-16s %-16s %s\n", d.ID, d.Type, d.Trust, d.Description)
	}
	return nil
}

func (r *runner) backlinks(root string, args []string) error {
	fs := newFlags("backlinks", r.errOut)
	asJSON := fs.Bool("json", false, "JSON output")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return codedf(ExitUsage, "usage: jot backlinks ID")
	}
	id, err := safeID(fs.Arg(0))
	if err != nil {
		return err
	}
	docs, _, err := loadConcepts(root, true)
	if err != nil {
		return err
	}
	graph := buildLinkGraph(root, docs)
	in := graph.Backlinks(id)
	var out []string
	for _, link := range graph.Out[id] {
		if link.TargetID != "" {
			out = append(out, link.TargetID)
		}
	}
	sort.Strings(out)
	if *asJSON {
		return jsonOut(r.out, map[string]any{"id": id, "backlinks": in, "links_to": dedupe(out)})
	}
	fmt.Fprintf(r.out, "Referenced by (%d):\n", len(in))
	for _, from := range in {
		fmt.Fprintf(r.out, "  %s\n", from)
	}
	fmt.Fprintf(r.out, "Links to (%d):\n", len(dedupe(out)))
	for _, to := range dedupe(out) {
		fmt.Fprintf(r.out, "  %s\n", to)
	}
	return nil
}

func (r *runner) log(root string, args []string) error {
	fs := newFlags("log", r.errOut)
	sinceText := fs.String("since", "", "only entries on or after this date or duration")
	capture := fs.String("capture", "", "only entries for this capture id")
	limit := fs.Int("limit", 20, "maximum entries (0 for all)")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	since, err := parseSince(*sinceText)
	if err != nil {
		return coded(ExitUsage, err)
	}
	entries, err := readLog(root)
	if err != nil {
		return err
	}
	entries = filterLog(entries, since, *capture, *limit)
	if *asJSON {
		return jsonOut(r.out, map[string]any{"entries": entries})
	}
	if len(entries) == 0 {
		fmt.Fprintln(r.out, "No log entries.")
		return nil
	}
	for _, e := range entries {
		fmt.Fprintf(r.out, "%s  %-8s %s\n", e.Date, e.Kind, e.Title)
		for _, d := range e.Details {
			fmt.Fprintf(r.out, "    %s\n", d)
		}
	}
	return nil
}

func (r *runner) openIn(root, mode string, args []string) error {
	fs := newFlags(mode, r.errOut)
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return codedf(ExitUsage, "usage: jot %s ID", mode)
	}
	path, err := resolveGetPath(root, fs.Arg(0))
	if err != nil {
		return err
	}
	abs := filepath.Join(root, filepath.FromSlash(path))
	if _, err := os.Stat(abs); err != nil {
		return err
	}
	envVar, fallback := "EDITOR", "vi"
	if mode == "open" {
		envVar, fallback = "PAGER", "less"
	}
	if mode == "edit" && strings.HasPrefix(path, "raw/") {
		return errors.New("raw captures are immutable; they cannot be edited")
	}
	program := os.Getenv(envVar)
	if strings.TrimSpace(program) == "" {
		program = fallback
	}
	parts := strings.Fields(program)
	cmd := exec.CommandContext(r.ctx, parts[0], append(parts[1:], abs)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	if mode == "edit" {
		return syncAfter(r.ctx, root, "jot: edit "+path, false)
	}
	return nil
}

func (r *runner) apply(root string, args []string) error {
	fs := newFlags("apply", r.errOut)
	stdin := fs.Bool("stdin", false, "read transaction JSON from stdin")
	dryRun := fs.Bool("dry-run", false, "validate and report without writing")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if !*stdin {
		return codedf(ExitUsage, "jot apply requires --stdin")
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	req, err := decodeApply(b)
	if err != nil {
		return coded(ExitUsage, err)
	}
	if *dryRun {
		req.DryRun = true
	}
	result, err := applyRequest(r.ctx, root, req)
	if err != nil {
		return err
	}
	if *asJSON {
		return jsonOut(r.out, result)
	}
	if result.DryRun {
		fmt.Fprintf(r.out, "Dry run OK: %d path changes would be written\n", len(result.Changed))
		for _, p := range result.Changed {
			fmt.Fprintf(r.out, "  %s\n", p)
		}
		return nil
	}
	fmt.Fprintf(r.out, "Published %d path changes at %s\n", len(result.Changed), result.Revision)
	return nil
}

func (r *runner) publish(root string, args []string) error {
	fs := newFlags("publish", r.errOut)
	captureID := fs.String("capture", "", "capture ID being compiled")
	summary := fs.String("summary", "publish Markdown edits", "commit/log summary")
	disposition := fs.String("disposition", "compiled", "compiled or no-material")
	allow := fs.Bool("allow-authoritative-change", false, "accept intentional authoritative block changes")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if _, err := refreshDerived(root, *allow); err != nil {
		return coded(ExitLintIssues, err)
	}
	if *captureID != "" {
		if *disposition != "compiled" && *disposition != "no-material" {
			return codedf(ExitUsage, "disposition must be compiled or no-material")
		}
		if err := setCaptureDisposition(root, *captureID, *disposition); err != nil {
			return err
		}
		if err := appendLog(root, "ingest", *summary, "Capture: "+*captureID, "Disposition: "+*disposition); err != nil {
			return err
		}
	} else if err := appendLog(root, "update", *summary); err != nil {
		return err
	}
	if err := syncAfter(r.ctx, root, "jot: "+*summary, *allow); err != nil {
		return err
	}
	fmt.Fprintf(r.out, "Published at %s\n", currentRevision(r.ctx, root))
	return nil
}

func (r *runner) promote(root string, args []string) error {
	fs := newFlags("promote", r.errOut)
	id := fs.String("id", "", "concept id to create, e.g. answers/retrieval-latency")
	queryID := fs.String("query-id", "", "recorded query id from jot context")
	queryText := fs.String("query", "", "the question this answers")
	title := fs.String("title", "", "page title")
	description := fs.String("description", "", "one-line description")
	from := fs.String("from", "", "comma-separated concept ids that support the answer")
	pageType := fs.String("type", "Answer", "OKF type for the new page")
	stdin := fs.Bool("stdin", false, "read the answer body from stdin")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if !*stdin {
		return codedf(ExitUsage, "jot promote requires --stdin")
	}
	if strings.TrimSpace(*id) == "" {
		return codedf(ExitUsage, "jot promote requires --id")
	}
	safe, err := safeID(*id)
	if err != nil {
		return coded(ExitUsage, err)
	}
	body, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	req := PromoteRequest{
		ConceptID: safe, QueryID: *queryID, Query: *queryText, Title: *title,
		Description: *description, Body: string(body), Type: *pageType,
	}
	if *from != "" {
		for _, part := range strings.Split(*from, ",") {
			if s := strings.TrimSpace(part); s != "" {
				req.From = append(req.From, s)
			}
		}
	}
	if *queryID != "" {
		rec, ok := findQuery(root, *queryID)
		if !ok {
			return codedf(ExitUsage, "unknown query id %q; run jot context first", *queryID)
		}
		if req.Query == "" {
			req.Query = rec.Query
		}
		if len(req.From) == 0 {
			req.From = rec.Hits
		}
	}
	page, err := buildAnswerPage(root, req)
	if err != nil {
		return err
	}
	rel := filepath.ToSlash(filepath.Join("wiki", safe+".md"))
	if _, err := validateConcept(rel, []byte(page)); err != nil {
		return err
	}
	dest := filepath.Join(root, filepath.FromSlash(rel))
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("%s already exists; choose another --id", rel)
	}
	if err := atomicWrite(dest, []byte(page), 0o644); err != nil {
		return err
	}
	if err := appendLog(root, "promote", "promote answer to "+safe, "Query: "+req.Query); err != nil {
		return err
	}
	if *queryID != "" {
		markPromoted(root, *queryID, safe)
	}
	if err := syncAfter(r.ctx, root, "jot: promote answer to "+safe, false); err != nil {
		return err
	}
	if *asJSON {
		return jsonOut(r.out, map[string]any{"id": safe, "path": rel, "revision": currentRevision(r.ctx, root)})
	}
	fmt.Fprintf(r.out, "Promoted answer to %s\n", rel)
	return nil
}

func (r *runner) maintain(root string, args []string) error {
	fs := newFlags("maintain", r.errOut)
	asJSON := fs.Bool("json", false, "JSON output")
	drain := fs.Int("drain", 0, "emit up to N model-tier work orders")
	kind := fs.String("kind", "", "restrict to one finding kind")
	resolve := fs.Bool("resolve", false, "read verdicts from stdin")
	stdin := fs.Bool("stdin", false, "read from stdin (with --resolve)")
	checkURLs := fs.Bool("check-urls", false, "also check that source URLs resolve")
	rescan := fs.Bool("rescan", true, "run the deterministic scan first")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	if *resolve {
		if !*stdin {
			return codedf(ExitUsage, "jot maintain --resolve requires --stdin")
		}
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		var resolutions []Resolution
		if err := json.Unmarshal(b, &resolutions); err != nil {
			var single Resolution
			if err2 := json.Unmarshal(b, &single); err2 != nil {
				return codedf(ExitUsage, "expected a JSON array of {finding_id, verdict}: %v", err)
			}
			resolutions = []Resolution{single}
		}
		report, err := resolveFindings(root, resolutions)
		if err != nil {
			return coded(ExitUsage, err)
		}
		if *asJSON {
			return jsonOut(r.out, report)
		}
		fmt.Fprintf(r.out, "Applied %d verdicts (%d cached)\n", report.Applied, report.Cached)
		for _, id := range report.Unknown {
			fmt.Fprintf(r.errOut, "unknown finding: %s\n", id)
		}
		return nil
	}

	if *rescan {
		if _, err := scanVault(root, maintainOptions{CheckURLs: *checkURLs}); err != nil {
			return err
		}
	}

	if *drain > 0 {
		batch, err := drainWorkOrders(r.ctx, root, *drain, *kind)
		if err != nil {
			return err
		}
		if *asJSON || len(batch.Orders) > 0 {
			return jsonOut(r.out, batch)
		}
		fmt.Fprintln(r.out, "No model-tier work is queued.")
		return nil
	}

	state, err := loadMaintainState(root)
	if err != nil {
		return err
	}
	findings := openFindings(state, *kind, 0)
	if *asJSON {
		return jsonOut(r.out, map[string]any{
			"last_scan":       state.LastScan,
			"findings":        findings,
			"cached_verdicts": len(state.Verdicts),
		})
	}
	if len(findings) == 0 {
		fmt.Fprintln(r.out, "Nothing to maintain.")
		return nil
	}
	byKind := map[string]int{}
	model := 0
	for _, f := range findings {
		byKind[f.Kind]++
		if f.NeedsModel {
			model++
		}
	}
	kinds := make([]string, 0, len(byKind))
	for k := range byKind {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	fmt.Fprintf(r.out, "%d open findings (%d need judgement; %d verdicts cached)\n\n",
		len(findings), model, len(state.Verdicts))
	for _, k := range kinds {
		fmt.Fprintf(r.out, "  %-24s %d\n", k, byKind[k])
	}
	fmt.Fprintln(r.out, "\nMost recent:")
	for i, f := range findings {
		if i >= 10 {
			break
		}
		target := strings.Join(f.Concepts, ", ")
		if target == "" {
			target = "-"
		}
		fmt.Fprintf(r.out, "  [%s] %s: %s (%s)\n", f.Severity, f.Kind, f.Detail, target)
	}
	if model > 0 {
		fmt.Fprintf(r.out, "\nRun jot maintain --drain %d --json to hand the judgement calls to a harness.\n", min(model, 20))
	}
	return nil
}

func (r *runner) export(root string, args []string) error {
	fs := newFlags("export", r.errOut)
	asJSON := fs.Bool("json", false, "JSON output")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return codedf(ExitUsage, "usage: jot export DIRECTORY")
	}
	result, err := exportOKF(root, fs.Arg(0))
	if err != nil {
		return err
	}
	if *asJSON {
		return jsonOut(r.out, result)
	}
	fmt.Fprintf(r.out, "Exported %d concepts as an OKF %s bundle to %s\n", result.Concepts, okfVersion, result.Destination)
	return nil
}

func (r *runner) importBundle(root string, args []string) error {
	fs := newFlags("import", r.errOut)
	prefix := fs.String("prefix", "", "wiki subdirectory to import into")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return codedf(ExitUsage, "usage: jot import --prefix TOPIC DIRECTORY")
	}
	result, err := importOKF(root, fs.Arg(0), *prefix)
	if err != nil {
		return err
	}
	if err := syncAfter(r.ctx, root, fmt.Sprintf("jot: import OKF bundle into %s", result.Prefix), false); err != nil {
		return err
	}
	if *asJSON {
		return jsonOut(r.out, result)
	}
	fmt.Fprintf(r.out, "Imported %d concepts into wiki/%s (%d skipped)\n", len(result.Imported), result.Prefix, len(result.Skipped))
	for _, s := range result.Skipped {
		fmt.Fprintf(r.errOut, "  skipped %s\n", s)
	}
	return nil
}

func (r *runner) sync(args []string) error {
	fs := newFlags("sync", r.errOut)
	cont := fs.Bool("continue", false, "continue a resolved rebase")
	allow := fs.Bool("allow-authoritative-change", false, "accept intentional authoritative block changes")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	return r.withVault(true, *allow, func(root string) error {
		if *cont {
			if err := continueSync(r.ctx, root, *allow); err != nil {
				return err
			}
		} else {
			if err := syncBefore(r.ctx, root, *allow); err != nil {
				return err
			}
			if err := syncAfter(r.ctx, root, "jot: synchronize local edits", *allow); err != nil {
				return err
			}
		}
		fmt.Fprintf(r.out, "Synchronized at %s\n", currentRevision(r.ctx, root))
		return nil
	})
}

func (r *runner) status(root string, args []string) error {
	fs := newFlags("status", r.errOut)
	asJSON := fs.Bool("json", false, "JSON output")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	pending, err := pendingCaptures(root)
	if err != nil {
		return err
	}
	dirty := false
	if isGitRepo(r.ctx, root) {
		dirty, _ = gitDirty(r.ctx, root)
	}
	state, err := loadMaintainState(root)
	if err != nil {
		return err
	}
	open := openFindings(state, "", 0)
	model := 0
	for _, f := range open {
		if f.NeedsModel {
			model++
		}
	}
	rebase := rebaseInProgress(root)
	result := map[string]any{
		"vault": root, "revision": currentRevision(r.ctx, root), "dirty": dirty,
		"rebase_in_progress": rebase, "pending_captures": len(pending),
		"maintenance_open": len(open), "maintenance_needs_model": model,
		"last_scan": state.LastScan,
	}
	if *asJSON {
		if err := jsonOut(r.out, result); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(r.out, "Vault: %s\nRevision: %s\nDirty: %v\nPending captures: %d\nMaintenance: %d open (%d need judgement)\n",
			root, result["revision"], dirty, len(pending), len(open), model)
	}
	if rebase {
		return codedf(ExitConflict, "a Git rebase is in progress; resolve it and run jot sync --continue")
	}
	return nil
}

func (r *runner) lint(root string, args []string) error {
	fs := newFlags("lint", r.errOut)
	asJSON := fs.Bool("json", false, "JSON output")
	fix := fs.Bool("fix", false, "repair what can be repaired mechanically")
	allow := fs.Bool("allow-authoritative-change", false, "accept intentional authoritative block changes")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *fix {
		fixes, err := lintFix(root, *allow)
		if err != nil {
			return err
		}
		if !*asJSON {
			for _, f := range fixes {
				fmt.Fprintf(r.out, "fixed: %s\n", f)
			}
		}
	}
	result, err := validateVault(root, *allow)
	if *asJSON {
		_ = jsonOut(r.out, result)
	} else if err != nil {
		for _, issue := range result.Issues {
			fmt.Fprintln(r.errOut, issue)
		}
	}
	if err != nil {
		return coded(ExitLintIssues, errors.New("vault has lint findings"))
	}
	if !*asJSON {
		fmt.Fprintf(r.out, "Valid: %d concepts, %d captures, %d authoritative blocks\n",
			result.Concepts, result.Captures, result.Authoritative)
	}
	return nil
}

// lintFix repairs only the mechanical problems: regenerated indexes, a stale
// lexical cache, and manifest entries whose capture files are gone. Anything
// requiring judgement stays a finding.
func lintFix(root string, allow bool) ([]string, error) {
	var fixed []string
	docs, _, err := loadConcepts(root, false)
	if err != nil {
		return nil, err
	}
	if err := generateIndex(root, docs); err != nil {
		return nil, err
	}
	fixed = append(fixed, "regenerated wiki/index.md and per-topic indexes")

	if err := dropLexIndex(root); err != nil {
		return nil, err
	}
	fixed = append(fixed, "dropped the disposable lexical index")

	m, err := loadManifest(root)
	if err != nil {
		return nil, err
	}
	changed := false
	for id, rec := range m.Captures {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rec.Path))); os.IsNotExist(err) {
			delete(m.Captures, id)
			fixed = append(fixed, "removed manifest entry for missing capture "+id)
			changed = true
		}
	}
	if changed {
		if err := saveManifest(root, m); err != nil {
			return nil, err
		}
	}
	if _, err := refreshDerived(root, allow); err != nil {
		// Reported by the lint pass that follows.
		return fixed, nil
	}
	return fixed, nil
}

func (r *runner) completion(args []string) error {
	fs := newFlags("completion", r.errOut)
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return codedf(ExitUsage, "usage: jot completion bash|zsh|fish")
	}
	names := strings.Join(commandNames, " ")
	switch fs.Arg(0) {
	case "bash":
		fmt.Fprintf(r.out, `# jot bash completion: eval "$(jot completion bash)"
_jot() {
  local cur="${COMP_WORDS[COMP_CWORD]}"
  if [ "$COMP_CWORD" -eq 1 ]; then
    COMPREPLY=($(compgen -W "%s" -- "$cur"))
    return
  fi
  case "${COMP_WORDS[1]}" in
    get|open|edit|backlinks)
      COMPREPLY=($(compgen -W "$(jot index 2>/dev/null | awk '{print $1}')" -- "$cur")) ;;
    *)
      COMPREPLY=($(compgen -W "--json --stdin --limit --type --path --since --full --dry-run" -- "$cur")) ;;
  esac
}
complete -F _jot jot
`, names)
	case "zsh":
		fmt.Fprintf(r.out, `# jot zsh completion: eval "$(jot completion zsh)"
_jot() {
  local -a cmds
  cmds=(%s)
  if (( CURRENT == 2 )); then
    compadd -- $cmds
    return
  fi
  case ${words[2]} in
    get|open|edit|backlinks) compadd -- ${(f)"$(jot index 2>/dev/null | awk '{print $1}')"} ;;
    *) compadd -- --json --stdin --limit --type --path --since --full --dry-run ;;
  esac
}
compdef _jot jot
`, names)
	case "fish":
		fmt.Fprintf(r.out, `# jot fish completion: jot completion fish | source
complete -c jot -f
for cmd in %s
  complete -c jot -n "__fish_use_subcommand" -a $cmd
end
complete -c jot -n "__fish_seen_subcommand_from get open edit backlinks" -a "(jot index 2>/dev/null | awk '{print $1}')"
`, names)
	default:
		return codedf(ExitUsage, "unsupported shell %q; use bash, zsh, or fish", fs.Arg(0))
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
