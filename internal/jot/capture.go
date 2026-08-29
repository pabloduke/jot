package jot

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// Capture is an immutable source record.
type Capture struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	Title       string `json:"title"`
	SourceKind  string `json:"source_kind"`
	SourceURL   string `json:"source_url,omitempty"`
	Status      string `json:"status"`
	Disposition string `json:"disposition,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
}

func addCapture(root, title, sourceKind, sourceURL, content string, tags []string) (Capture, error) {
	if !utf8.ValidString(content) {
		return Capture{}, fmt.Errorf("capture content must be UTF-8 text; binary and rich media are not supported in v1")
	}
	if strings.TrimSpace(content) == "" && sourceURL == "" {
		return Capture{}, fmt.Errorf("capture content is empty")
	}
	if sourceURL != "" {
		u, err := url.ParseRequestURI(sourceURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return Capture{}, fmt.Errorf("source URL must use http or https")
		}
	}
	now := time.Now().UTC()
	if v := os.Getenv("JOT_TEST_NOW"); v != "" {
		parsed, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			return Capture{}, fmt.Errorf("JOT_TEST_NOW: %w", err)
		}
		now = parsed
	}
	suffix, err := randomSuffix(6)
	if err != nil {
		return Capture{}, err
	}
	id := now.Format("20060102T150405.000000000Z") + "-" + suffix
	if title == "" {
		if sourceURL != "" && strings.TrimSpace(content) == "" {
			title = sourceURL
		} else {
			title = excerpt(content, 72)
		}
	}
	if sourceKind == "" {
		sourceKind = "message"
	}
	stamp := now.Format(time.RFC3339Nano)

	meta := map[string]any{
		"type":        "Capture",
		"title":       title,
		"description": "Immutable source capture: " + title,
		"status":      StatusStable,
		"generated":   map[string]any{"by": ActorProcess, "at": stamp},
		"capture_id":  id,
		"source_kind": sourceKind,
		// Retained alongside generated.at so pre-OKF tooling keeps working.
		"timestamp": stamp,
	}
	if sourceURL != "" {
		meta["source_url"] = sourceURL
		meta["resource"] = sourceURL
		meta["sources"] = []any{map[string]any{
			"id":       "source",
			"resource": sourceURL,
			"title":    title,
		}}
	}
	if len(tags) > 0 {
		clean := make([]any, 0, len(tags))
		for _, tag := range tags {
			if t := strings.TrimSpace(tag); t != "" {
				clean = append(clean, t)
			}
		}
		if len(clean) > 0 {
			meta["tags"] = clean
		}
	}
	front, err := marshalFrontmatter(meta)
	if err != nil {
		return Capture{}, err
	}

	var out strings.Builder
	out.WriteString("---\n")
	out.WriteString(front)
	out.WriteString("---\n\n# Source\n\n")
	if strings.TrimSpace(content) == "" {
		out.WriteString("Content pending retrieval by an AI harness.\n")
	} else {
		out.WriteString(content)
		if !strings.HasSuffix(content, "\n") {
			out.WriteByte('\n')
		}
	}

	rel := filepath.ToSlash(filepath.Join("raw", "inbox", now.Format("2006"), now.Format("01"), id+".md"))
	b := []byte(out.String())
	if err := atomicWrite(filepath.Join(root, filepath.FromSlash(rel)), b, 0o644); err != nil {
		return Capture{}, err
	}
	m, err := loadManifest(root)
	if err != nil {
		return Capture{}, err
	}
	m.Captures[id] = CaptureRecord{
		Path: rel, SHA256: digestBytes(b), Status: "pending",
		SourceKind: sourceKind, SourceURL: sourceURL, CreatedAt: stamp,
	}
	if err := saveManifest(root, m); err != nil {
		return Capture{}, err
	}
	return Capture{ID: id, Path: rel, Title: title, SourceKind: sourceKind, SourceURL: sourceURL, Status: "pending", CreatedAt: stamp}, nil
}

// listCaptures returns captures with the given status, or all when status is
// empty or "all".
func listCaptures(root, status string) ([]Capture, error) {
	m, err := loadManifest(root)
	if err != nil {
		return nil, err
	}
	filter := status
	if filter == "all" {
		filter = ""
	}
	ids := sortedCaptureIDs(m, filter)
	result := make([]Capture, 0, len(ids))
	for _, id := range ids {
		rec := m.Captures[id]
		c := Capture{
			ID: id, Path: rec.Path, SourceKind: rec.SourceKind, SourceURL: rec.SourceURL,
			Status: rec.Status, Disposition: rec.Disposition, CreatedAt: rec.CreatedAt,
		}
		if b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rec.Path))); err == nil {
			if d, err := parseDocument(rec.Path, b); err == nil {
				c.Title = d.Title
			}
		}
		if c.Title == "" {
			c.Title = id
		}
		result = append(result, c)
	}
	return result, nil
}

func pendingCaptures(root string) ([]Capture, error) { return listCaptures(root, "pending") }

// reopenCapture returns a compiled capture to the pending queue so that a bad
// compilation can be redone.
func reopenCapture(root, id string) error {
	m, err := loadManifest(root)
	if err != nil {
		return err
	}
	rec, ok := m.Captures[id]
	if !ok {
		return fmt.Errorf("unknown capture %s", id)
	}
	if rec.Status == "pending" {
		return fmt.Errorf("capture %s is already pending", id)
	}
	rec.Status = "pending"
	rec.Disposition = ""
	m.Captures[id] = rec
	return saveManifest(root, m)
}
