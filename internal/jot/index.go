package jot

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// The lexical index is a disposable cache: it is gitignored, rebuilt on demand,
// and only ever accelerates work that plain file reads could redo. Entries are
// keyed by vault-relative path and invalidated on size or mtime change.
const lexIndexVersion = 2

type cachedChunk struct {
	Heading string         `json:"h,omitempty"`
	Excerpt string         `json:"e"`
	TF      map[string]int `json:"tf"`
	Len     int            `json:"n"`
}

type cacheEntry struct {
	ModTime   int64         `json:"mtime"`
	Size      int64         `json:"size"`
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Type      string        `json:"type"`
	Trust     string        `json:"trust"`
	Status    string        `json:"status"`
	Timestamp string        `json:"ts,omitempty"`
	Raw       bool          `json:"raw,omitempty"`
	Chunks    []cachedChunk `json:"chunks"`
}

type lexIndex struct {
	Version int                   `json:"version"`
	Entries map[string]cacheEntry `json:"entries"`
}

func indexPath(root string) string { return filepath.Join(root, ".jot", "index.json") }

func loadLexIndex(root string) *lexIndex {
	idx := &lexIndex{Version: lexIndexVersion, Entries: map[string]cacheEntry{}}
	b, err := os.ReadFile(indexPath(root))
	if err != nil {
		return idx
	}
	var stored lexIndex
	if err := json.Unmarshal(b, &stored); err != nil || stored.Version != lexIndexVersion {
		return idx
	}
	if stored.Entries == nil {
		stored.Entries = map[string]cacheEntry{}
	}
	return &stored
}

func saveLexIndex(root string, idx *lexIndex) error {
	b, err := json.Marshal(idx)
	if err != nil {
		return err
	}
	return atomicWrite(indexPath(root), append(b, '\n'), 0o644)
}

// corpusFile is one indexable file discovered on disk.
type corpusFile struct {
	rel  string
	abs  string
	raw  bool
	info os.FileInfo
}

func discoverCorpus(root string, includeRaw bool) ([]corpusFile, error) {
	var files []corpusFile
	collect := func(dir string, raw bool) error {
		err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
				return nil
			}
			if !raw && isReservedName(entry.Name()) {
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
			files = append(files, corpusFile{rel: filepath.ToSlash(rel), abs: path, raw: raw, info: info})
			return nil
		})
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := collect(filepath.Join(root, "wiki"), false); err != nil {
		return nil, err
	}
	if includeRaw {
		if err := collect(filepath.Join(root, "raw", "inbox"), true); err != nil {
			return nil, err
		}
	}
	return files, nil
}

// buildCorpus returns every searchable chunk, reusing cached tokenizations for
// files whose size and mtime are unchanged since the last run.
func buildCorpus(root string, includeRaw bool) ([]Chunk, error) {
	files, err := discoverCorpus(root, includeRaw)
	if err != nil {
		return nil, err
	}
	idx := loadLexIndex(root)
	next := make(map[string]cacheEntry, len(files))
	dirty := false

	var chunks []Chunk
	for _, f := range files {
		entry, ok := idx.Entries[f.rel]
		if !ok || entry.ModTime != f.info.ModTime().UnixNano() || entry.Size != f.info.Size() {
			built, err := indexFile(root, f)
			if err != nil {
				// A file that will not parse simply contributes nothing to search.
				dirty = true
				continue
			}
			entry = built
			dirty = true
		}
		next[f.rel] = entry
		for _, c := range entry.Chunks {
			chunks = append(chunks, Chunk{
				ID:        entry.ID,
				Path:      f.rel,
				Title:     entry.Title,
				Type:      entry.Type,
				Trust:     entry.Trust,
				Status:    entry.Status,
				Timestamp: entry.Timestamp,
				Raw:       entry.Raw,
				Heading:   c.Heading,
				Excerpt:   c.Excerpt,
				tf:        c.TF,
				length:    c.Len,
			})
		}
	}
	if len(next) != len(idx.Entries) {
		dirty = true
	}
	if dirty {
		idx.Version = lexIndexVersion
		idx.Entries = next
		// A cache write failure must never fail a read.
		_ = saveLexIndex(root, idx)
	}
	return chunks, nil
}

func indexFile(root string, f corpusFile) (cacheEntry, error) {
	b, err := os.ReadFile(f.abs)
	if err != nil {
		return cacheEntry{}, err
	}
	d, err := parseDocument(f.rel, b)
	if err != nil {
		return cacheEntry{}, err
	}
	if strings.TrimSpace(d.Type) == "" && !f.raw {
		return cacheEntry{}, errors.New("missing type")
	}
	if f.raw {
		d.ID = fmString(d.Frontmatter, "capture_id")
		if d.ID == "" {
			d.ID = strings.TrimSuffix(filepath.Base(f.rel), ".md")
		}
	} else {
		wikiRel, err := filepath.Rel(filepath.Join(root, "wiki"), f.abs)
		if err != nil {
			return cacheEntry{}, err
		}
		d.ID = strings.TrimSuffix(filepath.ToSlash(wikiRel), ".md")
	}
	entry := cacheEntry{
		ModTime:   f.info.ModTime().UnixNano(),
		Size:      f.info.Size(),
		ID:        d.ID,
		Title:     d.Title,
		Type:      d.Type,
		Trust:     d.Trust,
		Status:    d.EffectiveStatus(),
		Timestamp: d.Timestamp(),
		Raw:       f.raw,
	}
	for _, c := range chunksForDocument(d) {
		entry.Chunks = append(entry.Chunks, cachedChunk{Heading: c.Heading, Excerpt: c.Excerpt, TF: c.tf, Len: c.length})
	}
	return entry, nil
}

// dropLexIndex removes the cache. Used when a rebuild is forced.
func dropLexIndex(root string) error {
	err := os.Remove(indexPath(root))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
