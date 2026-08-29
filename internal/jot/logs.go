package jot

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LogEntry is one parsed record from wiki/log.md.
type LogEntry struct {
	Date    string   `json:"date"`
	Kind    string   `json:"kind"`
	Title   string   `json:"title"`
	Details []string `json:"details,omitempty"`
	Capture string   `json:"capture,omitempty"`
}

// readLog parses the append-only knowledge log. Entries are written by
// appendLog as "## [YYYY-MM-DD] kind | title" followed by "- detail" lines.
func readLog(root string) ([]LogEntry, error) {
	b, err := os.ReadFile(filepath.Join(root, "wiki", "log.md"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []LogEntry
	var current *LogEntry
	for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "## [") {
			if current != nil {
				entries = append(entries, *current)
			}
			rest := strings.TrimPrefix(trim, "## [")
			date, remainder, _ := strings.Cut(rest, "]")
			kind, title, _ := strings.Cut(strings.TrimSpace(remainder), "|")
			current = &LogEntry{
				Date:  strings.TrimSpace(date),
				Kind:  strings.TrimSpace(kind),
				Title: strings.TrimSpace(title),
			}
			continue
		}
		if current == nil || !strings.HasPrefix(trim, "- ") {
			continue
		}
		detail := strings.TrimSpace(strings.TrimPrefix(trim, "- "))
		current.Details = append(current.Details, detail)
		if rest, ok := strings.CutPrefix(detail, "Capture: "); ok {
			current.Capture = strings.TrimSpace(rest)
		}
	}
	if current != nil {
		entries = append(entries, *current)
	}
	return entries, nil
}

// filterLog applies the jot log flags. Newest entries are returned first.
func filterLog(entries []LogEntry, since time.Time, capture string, limit int) []LogEntry {
	out := make([]LogEntry, 0, len(entries))
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if capture != "" && e.Capture != capture {
			continue
		}
		if !since.IsZero() {
			d, err := time.Parse("2006-01-02", e.Date)
			if err != nil || d.Before(since) {
				continue
			}
		}
		out = append(out, e)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}
