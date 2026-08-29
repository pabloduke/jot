package jot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Document is one OKF concept: YAML frontmatter plus a Markdown body.
type Document struct {
	ID          string         `json:"id"`
	Path        string         `json:"path"`
	Frontmatter map[string]any `json:"frontmatter"`
	Type        string         `json:"type"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Resource    string         `json:"resource,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Status      string         `json:"status,omitempty"`
	StaleAfter  string         `json:"stale_after,omitempty"`
	Generated   *Attestation   `json:"generated,omitempty"`
	Verified    []Attestation  `json:"verified,omitempty"`
	Sources     []Source       `json:"sources,omitempty"`
	Trust       string         `json:"trust"`
	Body        string         `json:"body,omitempty"`
}

// Timestamp reports when the concept was authored. OKF records this as
// generated.at; jot's pre-OKF timestamp field is accepted as a fallback.
func (d Document) Timestamp() string {
	if d.Generated != nil && d.Generated.At != "" {
		return d.Generated.At
	}
	return fmString(d.Frontmatter, "timestamp")
}

// EffectiveStatus defaults to stable, per OKF §Freshness.
func (d Document) EffectiveStatus() string {
	if d.Status == "" {
		return StatusStable
	}
	return d.Status
}

// IsStale reports whether stale_after has passed.
func (d Document) IsStale(now time.Time) bool {
	if d.StaleAfter == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, d.StaleAfter)
	if err != nil {
		return false
	}
	return now.After(t)
}

func splitFrontmatter(b []byte) (meta string, body string, err error) {
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	s = strings.TrimPrefix(s, "\ufeff")
	if !strings.HasPrefix(s, "---\n") {
		return "", "", errors.New("missing YAML frontmatter")
	}
	end := strings.Index(s[4:], "\n---")
	if end < 0 {
		return "", "", errors.New("unterminated YAML frontmatter")
	}
	end += 4
	rest := s[end+4:]
	rest = strings.TrimPrefix(rest, "\n")
	return s[4:end], rest, nil
}

func parseDocument(path string, b []byte) (Document, error) {
	meta, body, err := splitFrontmatter(b)
	if err != nil {
		return Document{}, err
	}
	raw, err := decodeFrontmatter(meta)
	if err != nil {
		return Document{}, err
	}
	d := Document{
		Path:        filepath.ToSlash(path),
		Frontmatter: raw,
		Type:        fmString(raw, "type"),
		Title:       fmString(raw, "title"),
		Description: fmString(raw, "description"),
		Resource:    fmString(raw, "resource"),
		Tags:        fmStrings(raw, "tags"),
		Status:      fmString(raw, "status"),
		StaleAfter:  fmString(raw, "stale_after"),
		Generated:   fmAttestation(raw, "generated"),
		Verified:    fmAttestations(raw, "verified"),
		Sources:     fmSources(raw, "sources"),
		Body:        body,
	}
	d.Trust = trustTier(d.Verified)
	if d.Title == "" {
		// OKF: consumers may derive the title from the filename.
		d.Title = titleFromPath(path)
	}
	return d, nil
}

func titleFromPath(path string) string {
	base := strings.TrimSuffix(filepath.Base(filepath.ToSlash(path)), ".md")
	base = strings.ReplaceAll(strings.ReplaceAll(base, "-", " "), "_", " ")
	if base == "" {
		return ""
	}
	return strings.ToUpper(base[:1]) + base[1:]
}

// parseConcept enforces only what OKF requires of every concept: parseable
// frontmatter carrying a non-empty type. Stricter jot conventions are reported
// by houseRuleIssues so that one unconventional page never breaks reads.
func parseConcept(path string, b []byte) (Document, error) {
	d, err := parseDocument(path, b)
	if err != nil {
		return Document{}, fmt.Errorf("%s: %w", path, err)
	}
	if strings.TrimSpace(d.Type) == "" {
		return Document{}, fmt.Errorf("%s: frontmatter is missing the required OKF field \"type\"", path)
	}
	return d, nil
}

// houseRuleIssues reports jot's own conventions on top of OKF conformance.
// These are lint findings, never read-path failures.
func houseRuleIssues(d Document) []string {
	var issues []string
	if strings.TrimSpace(fmString(d.Frontmatter, "title")) == "" {
		issues = append(issues, fmt.Sprintf("%s: missing frontmatter field \"title\"", d.Path))
	}
	if strings.TrimSpace(d.Description) == "" {
		issues = append(issues, fmt.Sprintf("%s: missing frontmatter field \"description\"", d.Path))
	}
	if ts := d.Timestamp(); strings.TrimSpace(ts) == "" {
		issues = append(issues, fmt.Sprintf("%s: missing generated.at (or legacy timestamp)", d.Path))
	} else if _, err := time.Parse(time.RFC3339, ts); err != nil {
		issues = append(issues, fmt.Sprintf("%s: generated.at must be ISO 8601, got %q", d.Path, ts))
	}
	switch d.EffectiveStatus() {
	case StatusDraft, StatusStable, StatusDeprecated:
	default:
		issues = append(issues, fmt.Sprintf("%s: status must be draft, stable, or deprecated, got %q", d.Path, d.Status))
	}
	if d.StaleAfter != "" {
		if _, err := time.Parse(time.RFC3339, d.StaleAfter); err != nil {
			issues = append(issues, fmt.Sprintf("%s: stale_after must be ISO 8601, got %q", d.Path, d.StaleAfter))
		}
	}
	for i, s := range d.Sources {
		if strings.TrimSpace(s.Resource) == "" {
			issues = append(issues, fmt.Sprintf("%s: sources[%d] is missing the required field \"resource\"", d.Path, i))
		}
	}
	if !strings.Contains(d.Body, "# ") {
		issues = append(issues, fmt.Sprintf("%s: concept body needs a Markdown heading", d.Path))
	}
	issues = append(issues, footnoteIssues(d)...)
	return issues
}

// footnoteIssues checks OKF per-claim attribution: every [^id] reference in the
// body must resolve to a declared sources[].id.
func footnoteIssues(d Document) []string {
	if len(d.Sources) == 0 && !strings.Contains(d.Body, "[^") {
		return nil
	}
	declared := map[string]bool{}
	for _, s := range d.Sources {
		if s.ID != "" {
			declared[s.ID] = true
		}
	}
	var issues []string
	seen := map[string]bool{}
	for _, match := range footnotePattern.FindAllStringSubmatch(d.Body, -1) {
		id := match[1]
		if seen[id] || declared[id] {
			continue
		}
		seen[id] = true
		issues = append(issues, fmt.Sprintf("%s: footnote [^%s] does not resolve to any sources[].id", d.Path, id))
	}
	return issues
}

func isReservedName(name string) bool { return name == "index.md" || name == "log.md" }

// loadConcepts walks the wiki and returns every page that meets OKF's minimum
// bar. Pages that do not parse are reported as issues instead of aborting the
// walk, so a single malformed file cannot take down search or the web UI.
func loadConcepts(root string, includeBody bool) ([]Document, []string, error) {
	wiki := filepath.Join(root, "wiki")
	var docs []Document
	var issues []string
	err := filepath.WalkDir(wiki, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" || isReservedName(entry.Name()) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			issues = append(issues, err.Error())
			return nil
		}
		d, err := parseConcept(filepath.ToSlash(rel), b)
		if err != nil {
			issues = append(issues, err.Error())
			return nil
		}
		idrel, _ := filepath.Rel(wiki, path)
		d.ID = strings.TrimSuffix(filepath.ToSlash(idrel), ".md")
		if !includeBody {
			d.Body = ""
		}
		docs = append(docs, d)
		return nil
	})
	if os.IsNotExist(err) {
		return nil, issues, nil
	}
	if err != nil {
		return nil, issues, err
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].ID < docs[j].ID })
	return docs, issues, nil
}

// listConcepts is the read-path helper: malformed pages are skipped silently so
// that callers always get the healthy remainder of the wiki.
func listConcepts(root string, includeBody bool) ([]Document, error) {
	docs, _, err := loadConcepts(root, includeBody)
	return docs, err
}

func topicOf(id string) string {
	if parts := strings.Split(id, "/"); len(parts) > 1 {
		return parts[0]
	}
	return ""
}

func topicLabel(topic string) string {
	if topic == "" {
		return "General"
	}
	label := strings.ReplaceAll(topic, "-", " ")
	return strings.ToUpper(label[:1]) + label[1:]
}

// generateIndex writes the bundle-root index.md plus one index.md per topic
// directory, giving OKF consumers progressive disclosure instead of a single
// flat listing.
func generateIndex(root string, docs []Document) error {
	byTopic := map[string][]Document{}
	for _, d := range docs {
		byTopic[topicOf(d.ID)] = append(byTopic[topicOf(d.ID)], d)
	}
	topics := make([]string, 0, len(byTopic))
	for topic := range byTopic {
		topics = append(topics, topic)
	}
	sort.Strings(topics)

	var out strings.Builder
	fmt.Fprintf(&out, "---\nokf_version: \"%s\"\n---\n\n# Jot Knowledge Index\n\n", okfVersion)
	if len(docs) == 0 {
		out.WriteString("No compiled concepts yet.\n")
	}
	for _, topic := range topics {
		fmt.Fprintf(&out, "## %s\n\n", topicLabel(topic))
		for _, d := range byTopic[topic] {
			rel := strings.TrimPrefix(d.Path, "wiki/")
			fmt.Fprintf(&out, "* [%s](%s) - %s\n", d.Title, rel, d.Description)
		}
		out.WriteByte('\n')
	}
	if err := atomicWrite(filepath.Join(root, "wiki", "index.md"), []byte(out.String()), 0o644); err != nil {
		return err
	}

	// Per-topic index.md for progressive disclosure.
	wanted := map[string]bool{}
	for _, topic := range topics {
		if topic == "" {
			continue
		}
		wanted[topic] = true
		var sub strings.Builder
		fmt.Fprintf(&sub, "# %s\n\n", topicLabel(topic))
		for _, d := range byTopic[topic] {
			name := filepath.Base(d.Path)
			fmt.Fprintf(&sub, "* [%s](%s) - %s\n", d.Title, name, d.Description)
		}
		p := filepath.Join(root, "wiki", filepath.FromSlash(topic), "index.md")
		if err := atomicWrite(p, []byte(sub.String()), 0o644); err != nil {
			return err
		}
	}
	return pruneTopicIndexes(root, wanted)
}

// pruneTopicIndexes removes index.md files left behind by emptied topics.
func pruneTopicIndexes(root string, wanted map[string]bool) error {
	wiki := filepath.Join(root, "wiki")
	entries, err := os.ReadDir(wiki)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || wanted[entry.Name()] {
			continue
		}
		p := filepath.Join(wiki, entry.Name(), "index.md")
		if _, err := os.Stat(p); err == nil {
			if err := os.Remove(p); err != nil {
				return err
			}
		}
	}
	return nil
}

const authorityMarker = "<!-- jot:authoritative -->"

func authoritativeBlocks(content string) ([]string, error) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	var hashes []string
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != authorityMarker {
			continue
		}
		i++
		for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
			i++
		}
		if i >= len(lines) || strings.TrimSpace(lines[i]) == authorityMarker {
			return nil, errors.New("authoritative marker has no following Markdown block")
		}
		start := i
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "```") || strings.HasPrefix(strings.TrimSpace(lines[i]), "~~~") {
			fence := strings.TrimSpace(lines[i])[:3]
			i++
			for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), fence) {
				i++
			}
			if i >= len(lines) {
				return nil, errors.New("unclosed fenced authoritative block")
			}
			i++
		} else {
			for i < len(lines) && strings.TrimSpace(lines[i]) != "" {
				i++
			}
		}
		block := strings.TrimSpace(strings.Join(lines[start:i], "\n"))
		hashes = append(hashes, digestBytes([]byte(block)))
		i--
	}
	return hashes, nil
}

// LintResult is the machine-readable outcome of jot lint.
type LintResult struct {
	Concepts      int      `json:"concepts"`
	Captures      int      `json:"captures"`
	Authoritative int      `json:"authoritative_blocks"`
	Issues        []string `json:"issues"`
}

func validateVault(root string, allowAuthorityChange bool) (LintResult, error) {
	m, err := loadManifest(root)
	if err != nil {
		return LintResult{}, err
	}
	result := LintResult{Captures: len(m.Captures)}
	registeredRaw := map[string]bool{}
	for id, rec := range m.Captures {
		registeredRaw[filepath.ToSlash(rec.Path)] = true
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rec.Path)))
		if err != nil {
			result.Issues = append(result.Issues, fmt.Sprintf("capture %s: %v", id, err))
			continue
		}
		if digestBytes(b) != rec.SHA256 {
			result.Issues = append(result.Issues, fmt.Sprintf("capture %s was modified; raw captures are immutable", id))
		}
	}
	rawInbox := filepath.Join(root, "raw", "inbox")
	if _, statErr := os.Stat(rawInbox); statErr == nil {
		_ = filepath.WalkDir(rawInbox, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				result.Issues = append(result.Issues, walkErr.Error())
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				rel, _ := filepath.Rel(root, path)
				result.Issues = append(result.Issues, fmt.Sprintf("raw source may not be a symlink: %s", filepath.ToSlash(rel)))
				return nil
			}
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			relSlash := filepath.ToSlash(rel)
			if !registeredRaw[relSlash] {
				result.Issues = append(result.Issues, fmt.Sprintf("unregistered raw capture: %s; add sources through jot add", relSlash))
			}
			return nil
		})
	}
	docs, loadIssues, err := loadConcepts(root, true)
	if err != nil {
		result.Issues = append(result.Issues, err.Error())
	}
	result.Issues = append(result.Issues, loadIssues...)
	result.Concepts = len(docs)

	graph := buildLinkGraph(root, docs)
	result.Issues = append(result.Issues, graph.Issues...)

	current := map[string]AuthorityRecord{}
	for _, d := range docs {
		result.Issues = append(result.Issues, houseRuleIssues(d)...)
		hashes, err := authoritativeBlocks(d.Body)
		if err != nil {
			result.Issues = append(result.Issues, fmt.Sprintf("%s: %v", d.Path, err))
			continue
		}
		for _, hash := range hashes {
			rec, ok := m.Authoritative[hash]
			if !ok {
				rec = AuthorityRecord{Path: d.Path}
			}
			rec.Path = d.Path
			current[hash] = rec
		}
	}
	result.Authoritative = len(current)
	if !allowAuthorityChange {
		for hash, rec := range m.Authoritative {
			if _, ok := current[hash]; !ok {
				result.Issues = append(result.Issues, fmt.Sprintf("authoritative block from %s changed or disappeared (fingerprint %.12s); rerun with --allow-authoritative-change if intentional", rec.Path, hash))
			}
		}
	}
	sort.Strings(result.Issues)
	if len(result.Issues) > 0 {
		return result, errors.New(strings.Join(result.Issues, "; "))
	}
	return result, nil
}

func refreshDerived(root string, allowAuthorityChange bool) (LintResult, error) {
	result, err := validateVault(root, allowAuthorityChange)
	if err != nil {
		return result, err
	}
	docs, err := listConcepts(root, false)
	if err != nil {
		return result, err
	}
	if err := generateIndex(root, docs); err != nil {
		return result, err
	}
	m, err := loadManifest(root)
	if err != nil {
		return result, err
	}
	now := ""
	current := map[string]bool{}
	for _, d := range docs {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(d.Path)))
		if err != nil {
			return result, err
		}
		hashes, err := authoritativeBlocks(string(b))
		if err != nil {
			return result, err
		}
		for _, hash := range hashes {
			current[hash] = true
			rec, ok := m.Authoritative[hash]
			if !ok {
				if now == "" {
					now = isoNow()
				}
				rec.CreatedAt = now
			}
			rec.Path = d.Path
			m.Authoritative[hash] = rec
		}
	}
	if allowAuthorityChange {
		for hash := range m.Authoritative {
			if !current[hash] {
				delete(m.Authoritative, hash)
			}
		}
	}
	return result, saveManifest(root, m)
}

func isoNow() string {
	if v := os.Getenv("JOT_TEST_NOW"); v != "" {
		return strings.TrimSpace(v)
	}
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func nowUTC() time.Time {
	if v := os.Getenv("JOT_TEST_NOW"); v != "" {
		if t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(v)); err == nil {
			return t.UTC()
		}
	}
	return time.Now().UTC()
}

// readDocument loads one vault-relative Markdown file as a concept document.
func readDocument(root, rel string) (Document, error) {
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return Document{}, err
	}
	d, err := parseDocument(rel, b)
	if err != nil {
		return Document{}, err
	}
	if strings.HasPrefix(rel, "wiki/") {
		d.ID = strings.TrimSuffix(strings.TrimPrefix(rel, "wiki/"), ".md")
	}
	return d, nil
}

// validateConcept applies OKF conformance plus jot's house rules. It guards
// content entering the vault through jot apply, where rejecting a malformed
// page outright is correct; read paths use parseConcept instead.
func validateConcept(path string, b []byte) (Document, error) {
	d, err := parseConcept(path, b)
	if err != nil {
		return Document{}, err
	}
	if issues := houseRuleIssues(d); len(issues) > 0 {
		return Document{}, errors.New(strings.Join(issues, "; "))
	}
	return d, nil
}
