package jot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type CaptureRecord struct {
	Path        string `json:"path"`
	SHA256      string `json:"sha256"`
	Status      string `json:"status"`
	SourceKind  string `json:"source_kind"`
	SourceURL   string `json:"source_url,omitempty"`
	CreatedAt   string `json:"created_at"`
	Disposition string `json:"disposition,omitempty"`
}

type AuthorityRecord struct {
	Path      string `json:"path"`
	CreatedAt string `json:"created_at"`
}

type Manifest struct {
	Version       int                        `json:"version"`
	Captures      map[string]CaptureRecord   `json:"captures"`
	Authoritative map[string]AuthorityRecord `json:"authoritative"`
}

func newManifest() Manifest {
	return Manifest{Version: 1, Captures: map[string]CaptureRecord{}, Authoritative: map[string]AuthorityRecord{}}
}

func loadManifest(root string) (Manifest, error) {
	p := filepath.Join(root, ".jot", "manifest.json")
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return newManifest(), nil
	}
	if err != nil {
		return Manifest{}, err
	}
	m := newManifest()
	if err := json.Unmarshal(b, &m); err != nil {
		return Manifest{}, err
	}
	if m.Captures == nil {
		m.Captures = map[string]CaptureRecord{}
	}
	if m.Authoritative == nil {
		m.Authoritative = map[string]AuthorityRecord{}
	}
	return m, nil
}

func saveManifest(root string, m Manifest) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return atomicWrite(filepath.Join(root, ".jot", "manifest.json"), b, 0o644)
}

func digestBytes(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func sortedCaptureIDs(m Manifest, status string) []string {
	ids := make([]string, 0)
	for id, rec := range m.Captures {
		if status == "" || rec.Status == status {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		a, _ := time.Parse(time.RFC3339Nano, m.Captures[ids[i]].CreatedAt)
		b, _ := time.Parse(time.RFC3339Nano, m.Captures[ids[j]].CreatedAt)
		return a.Before(b)
	})
	return ids
}
