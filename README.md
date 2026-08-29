# jot

`jot` is a personal, Git-backed note and knowledge system designed for both humans and AI harnesses. It stores immutable captures and a compiled, interlinked Markdown wiki. The files remain readable in any editor or Obsidian; a disposable lexical index makes them efficient to retrieve from the CLI.

Jot combines the [Open Knowledge Format](https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md) v0.2 convention—typed Markdown with YAML frontmatter, `index.md` hierarchies, and trust signals—with the [LLM Wiki](https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f) workflow: raw sources are compiled once into a persistent wiki that gets cleaner and more useful over time.

## Requirements

- Linux or macOS
- Go 1.25 or a downloaded release binary
- Git
- Authenticated [GitHub CLI](https://cli.github.com/) account

## Build and initialize

```sh
go build -buildvcs=false -o jot ./cmd/jot
./jot init YOUR_GITHUB_USER/jot-notes
```

Initialization creates a private GitHub repository. On another computer, install the CLI and run the same `jot init OWNER/REPO`; Jot clones the existing vault locally.

## Capture and recall

```sh
jot -m "Here is a summary of my work this session"
jot add notes.md
jot add --url https://example.com/article
jot pending
jot captures --status compiled
jot context --json "what have I learned about retrieval?"
```

For sensitive or multiline notes, prefer stdin so the content does not appear in shell history:

```sh
jot add --stdin < session-summary.md
```

URL content is fetched by the calling AI harness and passed with `--stdin --url URL`. A URL without content is saved as a pending provenance stub.

### Narrowing retrieval

`jot context` ranks passages with BM25 over a cache in `.jot/index.json`, which is rebuilt automatically whenever a file's size or mtime changes and is safe to delete at any time.

```sh
jot context --type Concept --path systems/ "retrieval"
jot context --since 2026-06-01 --limit 20 "release process"
jot context --full "release process"        # whole pages, not excerpts
jot context --include-raw "half-remembered idea"   # also search uncompiled captures
```

## Browse the wiki

Serve the compiled wiki as a read-only, searchable website:

```sh
jot serve --port 8787
jot serve --watch 5m        # also re-synchronize with GitHub on an interval
```

`jot serve` binds `0.0.0.0` by default so the wiki is reachable from other devices on your network; pass `--bind 127.0.0.1` to restrict it to the local machine. Binding to every interface exposes compiled wiki pages to any device that can reach the port, so use it only on a trusted network. Raw captures and `.jot` metadata are never served.

Pages show backlinks, declared sources, trust tier, and status. Search results highlight matched terms.

## AI harness workflow

Tell any shell-capable harness to run `jot guide`. The normal loop is:

1. `jot pending --json`
2. `jot get CAPTURE_ID --json`
3. `jot context --json "relevant terms"`
4. Update the Markdown wiki directly and run `jot publish --capture CAPTURE_ID --summary "..."`, or send a complete JSON transaction to `jot apply --stdin --json`.

Validate a transaction before committing to it with `jot apply --stdin --dry-run`, which reports exactly what would change and writes nothing.

An apply transaction looks like:

```json
{
  "base_revision": "git-sha-from-jot-context",
  "capture_id": "20260829T120000.000000000Z-a1b2c3",
  "disposition": "compiled",
  "summary": "compile session notes about retrieval",
  "upserts": [
    {
      "id": "systems/retrieval",
      "content": "---\ntype: Concept\ntitle: Retrieval\ndescription: How relevant knowledge is selected.\ngenerated:\n  by: process:jot\n  at: 2026-08-29T12:00:00Z\n---\n\n# Retrieval\n\n...\n"
    }
  ]
}
```

## Concept format

The wiki is an OKF v0.2 bundle. OKF requires exactly one frontmatter field, `type`; jot additionally expects `title`, `description`, and an authorship stamp.

```yaml
---
type: Concept
title: Retrieval
description: How relevant knowledge is selected.
status: stable                    # draft | stable | deprecated
stale_after: 2027-01-01T00:00:00Z
tags: [search, okf]
generated:
  by: process:jot
  at: 2026-08-29T12:00:00Z
verified:
  - by: human:you
    at: 2026-08-29T13:00:00Z
sources:
  - id: spec
    resource: https://example.com/spec
    title: The spec
---
```

Unknown frontmatter keys are preserved. The legacy `timestamp` field is still accepted in place of `generated.at`.

**Trust tier is derived, never stored:** no `verified` entries means `unverified`, a non-human verifier means `machine-confirmed`, and a `human:` actor means `human-reviewed`.

Link with standard Markdown links or `[[wiki links]]`; both resolve and both produce backlinks. Per-claim attribution uses `[^id]` footnotes keyed to `sources[].id`, which `jot lint` verifies.

A page that fails jot's house rules is reported by `jot lint` but still loads: one malformed file never takes down search or the web UI. Only content entering through `jot apply` is rejected outright.

## Maintenance

`jot maintain` runs deterministic checks in Go. It costs nothing to run, so it is safe on every publish, and **it never edits the wiki**.

```sh
jot maintain                    # scan and summarize the queue
jot maintain --json             # the full queue
jot maintain --check-urls       # also verify that source URLs resolve
```

It finds orphans, broken and unresolved links, stale and deprecated pages, unresolved footnotes, thin pages, duplicate descriptions, lonely topics, long-unverified pages, and capture-integrity problems.

Items that need semantic judgement—contradiction candidates, near-duplicates, description drift—are queued rather than acted on. Hand a budgeted batch to a harness and feed the verdicts back:

```sh
jot maintain --drain 20 --json > orders.json
# ...answer each order...
jot maintain --resolve --stdin < verdicts.json
```

Two properties keep this cheap. Only pages that **changed** seed new questions, and each seed considers a handful of lexical neighbours rather than every other page, so the work is O(churn) rather than O(n²). Verdicts are then cached against the exact content they judged, so unchanged pages are never re-examined.

Acting on a verdict is a separate, explicit step through `jot apply`.

## Promoting answers

A good answer should stop being throwaway output. `jot context` journals each query locally (in `.jot/queries.json`, never in Git), and a worthwhile result can become a permanent page:

```sh
jot context --json "when do we deploy" | jq -r .query_id
echo "Deploys ship on Tuesdays." | jot promote --stdin --id answers/ship-day --query-id q-abc123
```

The new page is typed `Answer` and its `sources` point at the concepts that supported it.

## Sharing and interop

```sh
jot export ./bundle                      # portable OKF bundle: wiki only
jot import --prefix vendor ./their-bundle
```

Export contains only compiled concepts, the `index.md` hierarchy, and `log.md`—never raw captures or `.jot` state.

## Conflicts

Contradictions are recorded as structure rather than prose:

```yaml
conflicts:
  - systems/retrieval
```

Every entry must resolve to a real concept. `jot lint` enforces that, `jot maintain` keeps the contradiction queued as a warning until it is removed, and the wiki badges it on the page. A page whose whole purpose is to hold an unresolved disagreement can use `type: Conflict`.

## Authority

Human Markdown edits and agent edits are treated equally. To protect one statement, mark its next Markdown block:

```markdown
<!-- jot:authoritative -->
Markdown and Git are the canonical store for my knowledge.
```

Changing or removing a previously recorded authoritative block requires an explicit human override with `jot publish --allow-authoritative-change`.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | success |
| 1 | generic failure |
| 2 | the vault is readable but has lint findings |
| 3 | not initialized |
| 4 | stale `base_revision`, or an unresolved Git rebase |
| 64 | usage error |

## Shell completion

```sh
eval "$(jot completion bash)"     # or zsh
jot completion fish | source
```

## Development

```sh
GOCACHE=/tmp/jot-go-cache go test -race ./...
GOCACHE=/tmp/jot-go-cache go vet ./...
gofmt -l .
```
