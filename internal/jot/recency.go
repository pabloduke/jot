package jot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Recency answers "when did I last touch this?" from Git history rather than
// from frontmatter. generated.at is author-declared and drifts; commit dates
// are what actually happened. A single git log pass builds the whole map, and
// the result is cached against HEAD so it costs nothing until the vault moves.

type recencyEntry struct {
	Updated string `json:"updated"`
	Added   string `json:"added"`
}

type recencyCache struct {
	Version int                     `json:"version"`
	Head    string                  `json:"head"`
	Entries map[string]recencyEntry `json:"entries"`
	BuiltAt string                  `json:"built_at"`
	FromGit bool                    `json:"from_git"`
}

const recencyVersion = 1

// PageTimes carries the resolved timestamps for one page.
type PageTimes struct {
	Updated time.Time
	Added   time.Time
}

// Recency maps vault-relative paths to their edit history.
type Recency struct {
	times   map[string]PageTimes
	fromGit bool
}

func recencyPath(root string) string { return filepath.Join(root, ".jot", "recency.json") }

// loadRecency returns the edit-time map, rebuilding it when HEAD has moved.
// Failures degrade to filesystem mtimes rather than erroring: recency is a
// navigation nicety and must never break a page render.
func loadRecency(ctx context.Context, root string) *Recency {
	r := &Recency{times: map[string]PageTimes{}}
	head := currentRevision(ctx, root)

	if head != "" {
		if cached := readRecencyCache(root); cached != nil && cached.Head == head {
			r.fromGit = cached.FromGit
			for path, entry := range cached.Entries {
				r.times[path] = PageTimes{
					Updated: parseTimeOrZero(entry.Updated),
					Added:   parseTimeOrZero(entry.Added),
				}
			}
			r.fillFromMtimes(root)
			return r
		}
		if entries := gitRecency(ctx, root); entries != nil {
			r.fromGit = true
			for path, entry := range entries {
				r.times[path] = PageTimes{
					Updated: parseTimeOrZero(entry.Updated),
					Added:   parseTimeOrZero(entry.Added),
				}
			}
			writeRecencyCache(root, &recencyCache{
				Version: recencyVersion, Head: head, Entries: entries,
				BuiltAt: isoNow(), FromGit: true,
			})
		}
	}
	r.fillFromMtimes(root)
	return r
}

func parseTimeOrZero(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func readRecencyCache(root string) *recencyCache {
	b, err := os.ReadFile(recencyPath(root))
	if err != nil {
		return nil
	}
	var c recencyCache
	if err := json.Unmarshal(b, &c); err != nil || c.Version != recencyVersion {
		return nil
	}
	if c.Entries == nil {
		c.Entries = map[string]recencyEntry{}
	}
	return &c
}

func writeRecencyCache(root string, c *recencyCache) {
	b, err := json.Marshal(c)
	if err != nil {
		return
	}
	_ = atomicWrite(recencyPath(root), append(b, '\n'), 0o644)
}

// gitRecency walks the whole history once. Each commit prints its date on a
// marker line followed by the paths it touched, so the first date seen for a
// path is its last edit and the last date seen is when it first appeared.
func gitRecency(ctx context.Context, root string) map[string]recencyEntry {
	if !isGitRepo(ctx, root) {
		return nil
	}
	out, err := command(ctx, root, "git", "log", "--format=\x01%aI", "--name-only", "--no-merges")
	if err != nil {
		return nil
	}
	entries := map[string]recencyEntry{}
	current := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "\x01") {
			current = strings.TrimSpace(strings.TrimPrefix(line, "\x01"))
			continue
		}
		path := strings.TrimSpace(line)
		if path == "" || current == "" {
			continue
		}
		e, seen := entries[path]
		if !seen {
			e.Updated = current
		}
		e.Added = current // overwritten until the oldest commit wins
		entries[path] = e
	}
	return entries
}

// fillFromMtimes supplies times for files Git does not know about (never
// committed) and prefers a newer mtime over a stale commit date, so
// uncommitted edits still read as recent.
func (r *Recency) fillFromMtimes(root string) {
	wiki := filepath.Join(root, "wiki")
	_ = filepath.WalkDir(wiki, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		key := filepath.ToSlash(rel)
		mod := info.ModTime().UTC()
		t := r.times[key]
		if t.Updated.IsZero() || mod.After(t.Updated) {
			t.Updated = mod
		}
		if t.Added.IsZero() {
			t.Added = mod
		}
		r.times[key] = t
		return nil
	})
}

// Updated reports when a page was last edited.
func (r *Recency) Updated(path string) time.Time {
	if r == nil {
		return time.Time{}
	}
	return r.times[path].Updated
}

// Added reports when a page first appeared.
func (r *Recency) Added(path string) time.Time {
	if r == nil {
		return time.Time{}
	}
	return r.times[path].Added
}

// FromGit reports whether the map came from commit history rather than mtimes.
func (r *Recency) FromGit() bool { return r != nil && r.fromGit }

// DatedDoc pairs a document with its resolved edit time.
type DatedDoc struct {
	Doc     Document
	Updated time.Time
}

// MostRecent returns the n most recently edited documents.
func (r *Recency) MostRecent(docs []Document, n int) []DatedDoc {
	return r.sorted(docs, n, true)
}

// LeastRecent returns the n documents untouched for the longest, which is the
// list that answers "what did I forget about?".
func (r *Recency) LeastRecent(docs []Document, n int) []DatedDoc {
	return r.sorted(docs, n, false)
}

func (r *Recency) sorted(docs []Document, n int, newestFirst bool) []DatedDoc {
	out := make([]DatedDoc, 0, len(docs))
	for _, d := range docs {
		out = append(out, DatedDoc{Doc: d, Updated: r.Updated(d.Path)})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Updated.Equal(out[j].Updated) {
			return out[i].Doc.ID < out[j].Doc.ID
		}
		if newestFirst {
			return out[i].Updated.After(out[j].Updated)
		}
		return out[i].Updated.Before(out[j].Updated)
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}

// humanAge renders a compact relative age such as "7 months ago".
func humanAge(t time.Time, now time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := now.Sub(t)
	if d < 0 {
		return "just now"
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute")
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "hour")
	case d < 30*24*time.Hour:
		return plural(int(d.Hours()/24), "day")
	case d < 365*24*time.Hour:
		return plural(int(d.Hours()/(24*30)), "month")
	default:
		return plural(int(d.Hours()/(24*365)), "year")
	}
}

func plural(n int, unit string) string {
	if n <= 1 {
		return "1 " + unit + " ago"
	}
	return itoa(n) + " " + unit + "s ago"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
