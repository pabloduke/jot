package jot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Queries are journalled locally rather than into wiki/log.md: recording every
// retrieval in Git would produce a commit per question. The journal is what
// makes a query promotable into a permanent page later.
const queryJournalLimit = 500

// QueryRecord is one recorded retrieval.
type QueryRecord struct {
	ID       string   `json:"id"`
	Query    string   `json:"query"`
	At       string   `json:"at"`
	Hits     []string `json:"hits,omitempty"`
	Promoted string   `json:"promoted,omitempty"`
}

type queryJournal struct {
	Version int           `json:"version"`
	Queries []QueryRecord `json:"queries"`
}

func queryJournalPath(root string) string { return filepath.Join(root, ".jot", "queries.json") }

func loadQueryJournal(root string) *queryJournal {
	j := &queryJournal{Version: 1}
	b, err := os.ReadFile(queryJournalPath(root))
	if err != nil {
		return j
	}
	if err := json.Unmarshal(b, j); err != nil {
		return &queryJournal{Version: 1}
	}
	return j
}

func saveQueryJournal(root string, j *queryJournal) error {
	if len(j.Queries) > queryJournalLimit {
		j.Queries = j.Queries[len(j.Queries)-queryJournalLimit:]
	}
	b, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(queryJournalPath(root), append(b, '\n'), 0o644)
}

// recordQuery appends to the journal. Failures are non-fatal: journalling must
// never break a retrieval.
func recordQuery(root, query string, hits []Chunk) string {
	j := loadQueryJournal(root)
	suffix, err := randomSuffix(6)
	if err != nil {
		return ""
	}
	rec := QueryRecord{ID: "q-" + suffix, Query: query, At: isoNow()}
	seen := map[string]bool{}
	for _, h := range hits {
		if !seen[h.ID] {
			seen[h.ID] = true
			rec.Hits = append(rec.Hits, h.ID)
		}
	}
	j.Queries = append(j.Queries, rec)
	if err := saveQueryJournal(root, j); err != nil {
		return ""
	}
	return rec.ID
}

func findQuery(root, id string) (QueryRecord, bool) {
	j := loadQueryJournal(root)
	for _, q := range j.Queries {
		if q.ID == id {
			return q, true
		}
	}
	return QueryRecord{}, false
}

func markPromoted(root, queryID, conceptID string) {
	j := loadQueryJournal(root)
	for i := range j.Queries {
		if j.Queries[i].ID == queryID {
			j.Queries[i].Promoted = conceptID
			_ = saveQueryJournal(root, j)
			return
		}
	}
}

// PromoteRequest turns a synthesised answer into a permanent concept page.
type PromoteRequest struct {
	ConceptID   string
	QueryID     string
	Query       string
	Title       string
	Description string
	Body        string
	From        []string
	Type        string
}

// buildAnswerPage renders an OKF Answer concept whose sources point at the
// wiki pages that fed the synthesis. This is the compounding step: a good
// answer stops being throwaway output and becomes part of the corpus.
func buildAnswerPage(root string, req PromoteRequest) (string, error) {
	if strings.TrimSpace(req.Body) == "" {
		return "", fmt.Errorf("promote requires answer content on stdin")
	}
	if req.Type == "" {
		req.Type = "Answer"
	}
	if req.Title == "" {
		req.Title = req.Query
	}
	if req.Title == "" {
		return "", fmt.Errorf("promote requires --title or a recorded query")
	}
	if req.Description == "" {
		req.Description = excerpt("Recorded answer: "+req.Query, 160)
	}
	stamp := isoNow()
	meta := map[string]any{
		"type":        req.Type,
		"title":       req.Title,
		"description": req.Description,
		"status":      StatusDraft,
		"generated":   map[string]any{"by": ActorProcess, "at": stamp},
		"timestamp":   stamp,
	}
	if req.Query != "" {
		meta["query"] = req.Query
	}
	var sources []any
	for _, id := range req.From {
		safe, err := safeID(id)
		if err != nil {
			return "", err
		}
		d, err := readDocument(root, "wiki/"+safe+".md")
		if err != nil {
			return "", fmt.Errorf("cited concept %s: %w", id, err)
		}
		sources = append(sources, map[string]any{
			"id":       filepath.Base(safe),
			"resource": "/" + safe + ".md",
			"title":    d.Title,
		})
	}
	if len(sources) > 0 {
		meta["sources"] = sources
	}
	front, err := marshalFrontmatter(meta)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	out.WriteString("---\n")
	out.WriteString(front)
	out.WriteString("---\n\n# ")
	out.WriteString(req.Title)
	out.WriteString("\n\n")
	if req.Query != "" {
		out.WriteString("> " + req.Query + "\n\n")
	}
	body := strings.TrimSpace(req.Body)
	out.WriteString(body)
	out.WriteString("\n")
	if len(req.From) > 0 {
		out.WriteString("\n# Citations\n\n")
		for _, id := range req.From {
			safe, _ := safeID(id)
			d, err := readDocument(root, "wiki/"+safe+".md")
			title := safe
			if err == nil && d.Title != "" {
				title = d.Title
			}
			fmt.Fprintf(&out, "* [%s](/%s.md)\n", title, safe)
		}
	}
	return out.String(), nil
}

func parseSince(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC(), nil
		}
	}
	if d, err := time.ParseDuration(value); err == nil {
		return nowUTC().Add(-d), nil
	}
	return time.Time{}, fmt.Errorf("cannot parse --since %q; use YYYY-MM-DD, RFC 3339, or a duration like 720h", value)
}
