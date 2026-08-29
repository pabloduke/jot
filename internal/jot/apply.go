package jot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Upsert struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}

type Move struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type ApplyRequest struct {
	BaseRevision string   `json:"base_revision,omitempty"`
	CaptureID    string   `json:"capture_id,omitempty"`
	Disposition  string   `json:"disposition,omitempty"`
	Summary      string   `json:"summary"`
	Upserts      []Upsert `json:"upserts,omitempty"`
	Moves        []Move   `json:"moves,omitempty"`
	Archives     []string `json:"archives,omitempty"`
	// DryRun validates the whole transaction and reports what would change
	// without touching the vault.
	DryRun bool `json:"dry_run,omitempty"`
}

type ApplyResult struct {
	Revision    string   `json:"revision,omitempty"`
	CaptureID   string   `json:"capture_id,omitempty"`
	Disposition string   `json:"disposition,omitempty"`
	Changed     []string `json:"changed"`
	DryRun      bool     `json:"dry_run,omitempty"`
}

type backup struct {
	path    string
	content []byte
	existed bool
}

func applyRequest(ctx context.Context, root string, req ApplyRequest) (ApplyResult, error) {
	if strings.TrimSpace(req.Summary) == "" {
		return ApplyResult{}, errors.New("apply summary is required")
	}
	if req.BaseRevision != "" && req.BaseRevision != currentRevision(ctx, root) {
		return ApplyResult{}, codedf(ExitConflict, "stale base_revision; refresh with jot context and retry")
	}
	if req.Disposition == "" && req.CaptureID != "" {
		req.Disposition = "compiled"
	}
	if req.Disposition != "" && req.CaptureID == "" {
		return ApplyResult{}, errors.New("disposition requires capture_id")
	}
	if req.Disposition != "" && req.Disposition != "compiled" && req.Disposition != "no-material" {
		return ApplyResult{}, fmt.Errorf("disposition must be compiled or no-material")
	}
	if req.Disposition == "no-material" && (len(req.Upserts) > 0 || len(req.Moves) > 0 || len(req.Archives) > 0) {
		return ApplyResult{}, errors.New("no-material disposition cannot include wiki changes")
	}

	type action struct{ from, to string }
	var writes []struct {
		path string
		data []byte
	}
	var moves []action
	var changed []string
	seen := map[string]bool{}
	for _, up := range req.Upserts {
		id, err := safeID(up.ID)
		if err != nil {
			return ApplyResult{}, err
		}
		rel := filepath.ToSlash(filepath.Join("wiki", id+".md"))
		if seen[rel] {
			return ApplyResult{}, fmt.Errorf("duplicate transaction target %s", rel)
		}
		seen[rel] = true
		if _, err := validateConcept(rel, []byte(up.Content)); err != nil {
			return ApplyResult{}, err
		}
		writes = append(writes, struct {
			path string
			data []byte
		}{filepath.Join(root, filepath.FromSlash(rel)), []byte(up.Content)})
		changed = append(changed, rel)
	}
	for _, mv := range req.Moves {
		from, err := safeID(mv.From)
		if err != nil {
			return ApplyResult{}, err
		}
		to, err := safeID(mv.To)
		if err != nil {
			return ApplyResult{}, err
		}
		moves = append(moves, action{filepath.Join(root, "wiki", filepath.FromSlash(from)+".md"), filepath.Join(root, "wiki", filepath.FromSlash(to)+".md")})
		changed = append(changed, "wiki/"+from+".md", "wiki/"+to+".md")
	}
	for _, rawID := range req.Archives {
		id, err := safeID(rawID)
		if err != nil {
			return ApplyResult{}, err
		}
		name := filepath.Base(id) + ".md"
		to := filepath.Join(root, "wiki", "archive", time.Now().UTC().Format("20060102")+"-"+name)
		moves = append(moves, action{filepath.Join(root, "wiki", filepath.FromSlash(id)+".md"), to})
		changed = append(changed, "wiki/"+id+".md", filepath.ToSlash(strings.TrimPrefix(to, root+string(filepath.Separator))))
	}

	if req.DryRun {
		return ApplyResult{
			Revision:    currentRevision(ctx, root),
			CaptureID:   req.CaptureID,
			Disposition: req.Disposition,
			Changed:     changed,
			DryRun:      true,
		}, nil
	}

	backupPaths := map[string]bool{}
	for _, w := range writes {
		backupPaths[w.path] = true
	}
	for _, mv := range moves {
		backupPaths[mv.from], backupPaths[mv.to] = true, true
	}
	for _, p := range []string{filepath.Join(root, ".jot", "manifest.json"), filepath.Join(root, "wiki", "index.md"), filepath.Join(root, "wiki", "log.md")} {
		backupPaths[p] = true
	}
	var backups []backup
	for p := range backupPaths {
		b, err := os.ReadFile(p)
		if err == nil {
			backups = append(backups, backup{path: p, content: b, existed: true})
		} else if os.IsNotExist(err) {
			backups = append(backups, backup{path: p})
		} else {
			return ApplyResult{}, err
		}
	}
	restore := func() {
		for _, b := range backups {
			if b.existed {
				_ = atomicWrite(b.path, b.content, 0o644)
			} else {
				_ = os.Remove(b.path)
			}
		}
	}
	for _, mv := range moves {
		if _, err := os.Stat(mv.from); err != nil {
			restore()
			return ApplyResult{}, fmt.Errorf("move source: %w", err)
		}
		if _, err := os.Stat(mv.to); err == nil {
			restore()
			return ApplyResult{}, fmt.Errorf("move target already exists: %s", mv.to)
		}
		if err := os.MkdirAll(filepath.Dir(mv.to), 0o755); err != nil {
			restore()
			return ApplyResult{}, err
		}
		if err := os.Rename(mv.from, mv.to); err != nil {
			restore()
			return ApplyResult{}, err
		}
	}
	for _, w := range writes {
		if err := atomicWrite(w.path, w.data, 0o644); err != nil {
			restore()
			return ApplyResult{}, err
		}
	}
	if _, err := refreshDerived(root, false); err != nil {
		restore()
		return ApplyResult{}, err
	}
	if req.CaptureID != "" {
		if err := setCaptureDisposition(root, req.CaptureID, req.Disposition); err != nil {
			restore()
			return ApplyResult{}, err
		}
		if err := appendLog(root, "ingest", req.Summary, "Capture: "+req.CaptureID, "Disposition: "+req.Disposition); err != nil {
			restore()
			return ApplyResult{}, err
		}
	} else if err := appendLog(root, "update", req.Summary); err != nil {
		restore()
		return ApplyResult{}, err
	}
	if err := syncAfter(ctx, root, "jot: "+req.Summary, false); err != nil {
		return ApplyResult{Revision: currentRevision(ctx, root), CaptureID: req.CaptureID, Disposition: req.Disposition, Changed: changed}, err
	}
	return ApplyResult{Revision: currentRevision(ctx, root), CaptureID: req.CaptureID, Disposition: req.Disposition, Changed: changed}, nil
}

func setCaptureDisposition(root, id, disposition string) error {
	m, err := loadManifest(root)
	if err != nil {
		return err
	}
	rec, ok := m.Captures[id]
	if !ok {
		return fmt.Errorf("unknown capture %s", id)
	}
	if rec.Status != "pending" && rec.Status != disposition {
		return fmt.Errorf("capture %s is already %s", id, rec.Status)
	}
	rec.Status = disposition
	rec.Disposition = disposition
	m.Captures[id] = rec
	return saveManifest(root, m)
}

func decodeApply(b []byte) (ApplyRequest, error) {
	var req ApplyRequest
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return ApplyRequest{}, err
	}
	return req, nil
}
