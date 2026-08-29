package jot

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const initialIndex = `---
okf_version: "0.2"
---

# Jot Knowledge Index

No compiled concepts yet.
`

const initialLog = `# Jot Knowledge Log
`

const gitignoreBody = `.DS_Store
*.swp
*.tmp
.jot/lock
.jot/index.json
.jot/maintain.json
.jot/queries.json
`

const jotGuide = `# Jot Agent Contract

Jot is a personal, Git-backed knowledge system. Markdown and Git are canonical.
The wiki is an Open Knowledge Format (OKF) v0.2 bundle.

## Layers

- raw/inbox contains immutable source captures. Never edit or delete these files.
- wiki contains the compiled, OKF-compatible knowledge base. Agents primarily
  maintain it; human Markdown edits are ordinary equal contributions.
- .jot holds CLI-owned state. index.json, maintain.json and queries.json are
  disposable caches; manifest.json is not.

## Ingest workflow

1. Run jot pending --json and read one capture at a time.
2. Run jot context with the capture's important terms. Search before creating a
   concept. Narrow with --type, --path and --since; use --full for whole pages.
3. Update existing concepts when possible. Create wiki/<topic>/<slug>.md only
   for a genuinely distinct concept.
4. Preserve provenance, citations, contradictions, cross-links, and every
   <!-- jot:authoritative --> block.
5. Publish with jot apply (validate first with --dry-run), or edit directly and
   run jot publish --capture ID --summary TEXT.

## Concept frontmatter

OKF requires exactly one field: type. Jot additionally expects title,
description, and an authorship stamp. Prefer the OKF spelling:

    ---
    type: Concept
    title: Retrieval
    description: How relevant knowledge is selected.
    status: stable            # draft | stable | deprecated
    stale_after: 2027-01-01T00:00:00Z
    generated:
      by: process:jot
      at: 2026-08-29T12:00:00Z
    verified:
      - by: human:you
        at: 2026-08-29T13:00:00Z
    sources:
      - id: paper
        resource: https://example.com/paper
        title: The paper
    ---

The legacy timestamp field is still accepted in place of generated.at. Trust
tier is derived, never stored: no verified entries means unverified, a
non-human verifier means machine-confirmed, and a human: actor means
human-reviewed.

Link with standard Markdown links or [[wiki links]]; both resolve and both
produce backlinks. Per-claim attribution uses [^id] footnotes keyed to
sources[].id. Keep wiki/index.md, every wiki/<topic>/index.md, and wiki/log.md
CLI-owned.

## Maintenance loop

jot maintain runs deterministic checks only and costs nothing; it never edits
the wiki. Items needing judgement are queued, not acted on:

1. jot maintain --json                 inspect the queue
2. jot maintain --drain 20 --json      take a budgeted batch of work orders
3. answer each order, then pipe the array to jot maintain --resolve --stdin
4. act on the verdicts through jot apply, like any other change

Verdicts are cached against the exact content they judged, so unchanged pages
are never re-examined.

## Conflicts

Record a contradiction as structure, not prose. Add a conflicts list to the
frontmatter naming the concepts that disagree:

    conflicts:
      - systems/retrieval

Every entry must resolve to a real concept; jot lint enforces that, jot
maintain keeps the contradiction queued as a warning until it is removed, and
the web UI badges it. Use type: Conflict for a page whose whole purpose is to
hold an unresolved disagreement.

## Authority

The marker <!-- jot:authoritative --> applies to the next Markdown block. Never
remove it or alter that block's meaning. Record contrary evidence next to it as
a conflict.
`

const agentsPointer = `# Jot

Read JOT.md completely before modifying this knowledge repository. Use the jot
CLI for capture, context, validation, publishing, and synchronization.
`

func scaffoldVault(root string) error {
	dirs := []string{
		filepath.Join(root, "raw", "inbox"),
		filepath.Join(root, "wiki"),
		filepath.Join(root, ".jot"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	files := map[string]string{
		"wiki/index.md": initialIndex,
		"wiki/log.md":   initialLog,
		"JOT.md":        jotGuide,
		"AGENTS.md":     agentsPointer,
		"CLAUDE.md":     agentsPointer,
		".gitignore":    gitignoreBody,
	}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Stat(p); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := atomicWrite(p, []byte(content), 0o644); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".jot", "manifest.json")); os.IsNotExist(err) {
		if err := saveManifest(root, newManifest()); err != nil {
			return err
		}
	}
	return nil
}

func appendLog(root, kind, title string, lines ...string) error {
	p := filepath.Join(root, "wiki", "log.md")
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	entry := fmt.Sprintf("\n## [%s] %s | %s\n", time.Now().UTC().Format("2006-01-02"), kind, title)
	for _, line := range lines {
		entry += "- " + line + "\n"
	}
	return atomicWrite(p, append(b, []byte(entry)...), 0o644)
}
