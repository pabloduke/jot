package jot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ExportResult reports what an OKF export wrote.
type ExportResult struct {
	Destination string   `json:"destination"`
	Concepts    int      `json:"concepts"`
	Files       []string `json:"files"`
}

// exportOKF writes a self-contained OKF v0.2 bundle: wiki concepts, the
// index.md hierarchy, and log.md. Raw captures and .jot state are never
// exported, so the bundle is safe to share.
func exportOKF(root, dest string) (*ExportResult, error) {
	abs, err := filepath.Abs(dest)
	if err != nil {
		return nil, err
	}
	if entries, err := os.ReadDir(abs); err == nil && len(entries) > 0 {
		return nil, fmt.Errorf("export destination %s is not empty", abs)
	}
	docs, _, err := loadConcepts(root, true)
	if err != nil {
		return nil, err
	}
	result := &ExportResult{Destination: abs, Concepts: len(docs)}
	for _, d := range docs {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(d.Path)))
		if err != nil {
			return nil, err
		}
		rel := strings.TrimPrefix(d.Path, "wiki/")
		if err := atomicWrite(filepath.Join(abs, filepath.FromSlash(rel)), b, 0o644); err != nil {
			return nil, err
		}
		result.Files = append(result.Files, rel)
	}
	// Regenerate the index in the destination so okf_version and the per-topic
	// index.md files describe exactly what was exported.
	if err := os.MkdirAll(filepath.Join(abs, "wiki"), 0o755); err != nil {
		return nil, err
	}
	staging := filepath.Join(abs, ".jot-export-staging")
	if err := os.MkdirAll(filepath.Join(staging, "wiki"), 0o755); err != nil {
		return nil, err
	}
	defer os.RemoveAll(staging)
	for _, d := range docs {
		b, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(d.Path)))
		_ = atomicWrite(filepath.Join(staging, filepath.FromSlash(d.Path)), b, 0o644)
	}
	if err := generateIndex(staging, docs); err != nil {
		return nil, err
	}
	err = filepath.WalkDir(filepath.Join(staging, "wiki"), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() != "index.md" {
			return err
		}
		rel, err := filepath.Rel(filepath.Join(staging, "wiki"), path)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result.Files = append(result.Files, filepath.ToSlash(rel))
		return atomicWrite(filepath.Join(abs, rel), b, 0o644)
	})
	if err != nil {
		return nil, err
	}
	if b, err := os.ReadFile(filepath.Join(root, "wiki", "log.md")); err == nil {
		if err := atomicWrite(filepath.Join(abs, "log.md"), b, 0o644); err != nil {
			return nil, err
		}
		result.Files = append(result.Files, "log.md")
	}
	sort.Strings(result.Files)
	return result, nil
}

// ImportResult reports what an OKF import staged.
type ImportResult struct {
	Source   string   `json:"source"`
	Prefix   string   `json:"prefix"`
	Imported []string `json:"imported"`
	Skipped  []string `json:"skipped,omitempty"`
}

// importOKF copies an external OKF bundle into wiki/<prefix>/. Reserved files
// are skipped because jot owns index.md and log.md, and any concept that fails
// OKF's minimum bar is reported rather than written.
func importOKF(root, source, prefix string) (*ImportResult, error) {
	abs, err := filepath.Abs(source)
	if err != nil {
		return nil, err
	}
	if prefix == "" {
		return nil, errors.New("import requires --prefix, the wiki subdirectory to import into")
	}
	safePrefix, err := safeID(prefix)
	if err != nil {
		return nil, err
	}
	result := &ImportResult{Source: abs, Prefix: safePrefix}
	err = filepath.WalkDir(abs, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			return nil
		}
		rel, err := filepath.Rel(abs, path)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		if isReservedName(entry.Name()) {
			result.Skipped = append(result.Skipped, relSlash+" (reserved; jot regenerates it)")
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		target := filepath.ToSlash(filepath.Join("wiki", safePrefix, relSlash))
		if _, err := parseConcept(target, b); err != nil {
			result.Skipped = append(result.Skipped, relSlash+" ("+err.Error()+")")
			return nil
		}
		dest := filepath.Join(root, filepath.FromSlash(target))
		if _, err := os.Stat(dest); err == nil {
			result.Skipped = append(result.Skipped, relSlash+" (already exists)")
			return nil
		}
		if err := atomicWrite(dest, b, 0o644); err != nil {
			return err
		}
		result.Imported = append(result.Imported, target)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(result.Imported)
	sort.Strings(result.Skipped)
	return result, nil
}
