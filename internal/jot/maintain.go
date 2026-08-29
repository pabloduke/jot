package jot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Maintenance is deliberately split in two. Everything in this file is
// deterministic Go that costs nothing to run, so it executes on every publish.
// Findings marked NeedsModel are only ever *proposed*: jot emits a work order,
// an external harness forms a judgement, and the verdict comes back through
// jot maintain --resolve. Nothing here edits the wiki.

const maintainVersion = 1

// Finding kinds. Those in modelKinds require semantic judgement.
const (
	KindOrphan            = "orphan"
	KindBrokenLink        = "broken-link"
	KindStale             = "stale"
	KindDeprecated        = "deprecated"
	KindUnresolvedNote    = "unresolved-footnote"
	KindThinPage          = "thin-page"
	KindLonelyTopic       = "lonely-topic"
	KindMissingDesc       = "missing-description"
	KindDuplicateDesc     = "duplicate-description"
	KindDeadSource        = "dead-source"
	KindUnverified        = "unverified"
	KindOpenConflict      = "open-conflict"
	KindVaultIntegrity    = "vault-integrity"
	KindNearDuplicate     = "near-duplicate"
	KindContradiction     = "contradiction-candidate"
	KindDescriptionDrift  = "description-drift"
	SeverityInfo          = "info"
	SeverityWarn          = "warn"
	StatusOpen            = "open"
	StatusDispatched      = "dispatched"
	StatusResolved        = "resolved"
	StatusDismissed       = "dismissed"
	thinPageWords         = 40
	nearDuplicateFloor    = 0.82
	contradictionFloor    = 0.45
	contradictionNeighbor = 5
	unverifiedAfterDays   = 180
)

func modelKind(kind string) bool {
	switch kind {
	case KindNearDuplicate, KindContradiction, KindDescriptionDrift:
		return true
	}
	return false
}

// Finding is one maintenance item.
type Finding struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind"`
	Severity   string   `json:"severity"`
	NeedsModel bool     `json:"needs_model"`
	Concepts   []string `json:"concepts,omitempty"`
	Detail     string   `json:"detail"`
	Digests    []string `json:"digests,omitempty"`
	FoundAt    string   `json:"found_at"`
	Status     string   `json:"status"`
	Verdict    string   `json:"verdict,omitempty"`
	VerdictBy  string   `json:"verdict_by,omitempty"`
	VerdictAt  string   `json:"verdict_at,omitempty"`
	Note       string   `json:"note,omitempty"`
}

// VerdictRecord caches a judgement against the exact content it was made about.
// Re-scanning unchanged pages therefore never re-asks the same question.
type VerdictRecord struct {
	Verdict string `json:"verdict"`
	By      string `json:"by,omitempty"`
	At      string `json:"at"`
	Note    string `json:"note,omitempty"`
}

// MaintainState is the persisted queue and verdict cache.
type MaintainState struct {
	Version  int                      `json:"version"`
	LastScan string                   `json:"last_scan,omitempty"`
	Pages    map[string]string        `json:"pages"`
	Findings map[string]*Finding      `json:"findings"`
	Verdicts map[string]VerdictRecord `json:"verdicts"`
}

func newMaintainState() *MaintainState {
	return &MaintainState{
		Version:  maintainVersion,
		Pages:    map[string]string{},
		Findings: map[string]*Finding{},
		Verdicts: map[string]VerdictRecord{},
	}
}

func maintainPath(root string) string { return filepath.Join(root, ".jot", "maintain.json") }

func loadMaintainState(root string) (*MaintainState, error) {
	b, err := os.ReadFile(maintainPath(root))
	if os.IsNotExist(err) {
		return newMaintainState(), nil
	}
	if err != nil {
		return nil, err
	}
	s := newMaintainState()
	if err := json.Unmarshal(b, s); err != nil {
		return nil, fmt.Errorf("read maintain state: %w", err)
	}
	if s.Version != maintainVersion {
		return newMaintainState(), nil
	}
	if s.Pages == nil {
		s.Pages = map[string]string{}
	}
	if s.Findings == nil {
		s.Findings = map[string]*Finding{}
	}
	if s.Verdicts == nil {
		s.Verdicts = map[string]VerdictRecord{}
	}
	return s, nil
}

func saveMaintainState(root string, s *MaintainState) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(maintainPath(root), append(b, '\n'), 0o644)
}

func findingID(kind string, parts ...string) string {
	sorted := append([]string(nil), parts...)
	sort.Strings(sorted)
	sum := sha256.Sum256([]byte(kind + "\x00" + strings.Join(sorted, "\x00")))
	return kind + ":" + hex.EncodeToString(sum[:])[:12]
}

func verdictKey(kind string, digests ...string) string {
	sorted := append([]string(nil), digests...)
	sort.Strings(sorted)
	return kind + ":" + strings.Join(sorted, "+")
}

// MaintainReport summarises one scan.
type MaintainReport struct {
	Revision    string     `json:"revision,omitempty"`
	Scanned     int        `json:"concepts_scanned"`
	Opened      int        `json:"opened"`
	Closed      int        `json:"closed"`
	CacheHits   int        `json:"verdict_cache_hits"`
	OpenTotal   int        `json:"open_total"`
	ModelQueued int        `json:"model_queued"`
	Findings    []*Finding `json:"findings"`
}

type maintainOptions struct {
	CheckURLs bool
	Now       time.Time
}

// scanVault runs every deterministic check and reconciles the queue. It never
// contacts a model and never writes to the wiki.
func scanVault(root string, opts maintainOptions) (*MaintainReport, error) {
	if opts.Now.IsZero() {
		opts.Now = nowUTC()
	}
	state, err := loadMaintainState(root)
	if err != nil {
		return nil, err
	}
	docs, loadIssues, err := loadConcepts(root, true)
	if err != nil {
		return nil, err
	}
	graph := buildLinkGraph(root, docs)
	chunks, err := buildCorpus(root, false)
	if err != nil {
		return nil, err
	}
	vectors := pageVectors(chunks)

	report := &MaintainReport{Scanned: len(docs)}
	found := map[string]*Finding{}

	add := func(kind, severity, detail string, concepts []string, digests []string) {
		id := findingID(kind, concepts...)
		if _, ok := found[id]; ok {
			return
		}
		found[id] = &Finding{
			ID: id, Kind: kind, Severity: severity, NeedsModel: modelKind(kind),
			Concepts: concepts, Detail: detail, Digests: digests,
			FoundAt: opts.Now.Format(time.RFC3339), Status: StatusOpen,
		}
	}

	// cached reports whether this exact question has already been answered
	// about this exact content. On a hit the finding is restored to resolved
	// carrying the remembered verdict, so a revert closes the loop instead of
	// leaving a stale open item behind.
	cached := func(kind string, concepts, digests []string) bool {
		rec, ok := state.Verdicts[verdictKey(kind, digests...)]
		if !ok {
			return false
		}
		report.CacheHits++
		id := findingID(kind, concepts...)
		f, exists := state.Findings[id]
		if !exists {
			f = &Finding{
				ID: id, Kind: kind, Severity: SeverityInfo, NeedsModel: modelKind(kind),
				Concepts: concepts, FoundAt: opts.Now.Format(time.RFC3339),
			}
			state.Findings[id] = f
		}
		f.Digests = digests
		f.Status = StatusResolved
		f.Verdict = rec.Verdict
		f.VerdictBy = rec.By
		f.VerdictAt = rec.At
		f.Note = rec.Note
		return true
	}

	digests := map[string]string{}
	for _, d := range docs {
		digests[d.ID] = digestBytes([]byte(d.Body + "\x00" + d.Description + "\x00" + d.Title))
	}

	// --- structural checks -------------------------------------------------
	for _, issue := range loadIssues {
		add(KindVaultIntegrity, SeverityWarn, issue, nil, nil)
	}
	for _, issue := range graph.Issues {
		add(KindBrokenLink, SeverityWarn, issue, nil, nil)
	}
	for _, id := range graph.Orphans(docs) {
		add(KindOrphan, SeverityInfo, "no other page links here", []string{id}, []string{digests[id]})
	}

	descriptions := map[string][]string{}
	topics := map[string][]string{}
	for _, d := range docs {
		topics[topicOf(d.ID)] = append(topics[topicOf(d.ID)], d.ID)

		if strings.TrimSpace(d.Description) == "" {
			add(KindMissingDesc, SeverityWarn, "page has no description", []string{d.ID}, []string{digests[d.ID]})
		} else {
			key := strings.ToLower(strings.Join(strings.Fields(d.Description), " "))
			descriptions[key] = append(descriptions[key], d.ID)
		}
		if d.IsStale(opts.Now) {
			add(KindStale, SeverityWarn, "stale_after "+d.StaleAfter+" has passed", []string{d.ID}, []string{digests[d.ID]})
		}
		if d.EffectiveStatus() == StatusDeprecated {
			add(KindDeprecated, SeverityInfo, "page is marked deprecated", []string{d.ID}, []string{digests[d.ID]})
		}
		for _, other := range d.Conflicts {
			target := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(other), "/"), ".md")
			pair := []string{d.ID, target}
			sort.Strings(pair)
			add(KindOpenConflict, SeverityWarn, "a recorded contradiction is still open", pair, digestsFor(digests, pair))
		}
		for _, issue := range footnoteIssues(d) {
			add(KindUnresolvedNote, SeverityWarn, issue, []string{d.ID}, []string{digests[d.ID]})
		}
		if words := len(strings.Fields(stripMarkdown(d.Body))); words < thinPageWords {
			add(KindThinPage, SeverityInfo, fmt.Sprintf("page body is only %d words", words), []string{d.ID}, []string{digests[d.ID]})
		}
		if d.Trust == TrustUnverified {
			if ts, err := time.Parse(time.RFC3339, d.Timestamp()); err == nil {
				if opts.Now.Sub(ts) > unverifiedAfterDays*24*time.Hour {
					add(KindUnverified, SeverityInfo,
						fmt.Sprintf("never verified and last generated %s", ts.Format("2006-01-02")),
						[]string{d.ID}, []string{digests[d.ID]})
				}
			}
		}
	}
	for key, ids := range descriptions {
		if len(ids) > 1 {
			sort.Strings(ids)
			add(KindDuplicateDesc, SeverityWarn, "pages share the description "+key, ids, digestsFor(digests, ids))
		}
	}
	for topic, ids := range topics {
		if topic != "" && len(ids) == 1 {
			add(KindLonelyTopic, SeverityInfo, "topic "+topic+" holds a single page", ids, digestsFor(digests, ids))
		}
	}
	if opts.CheckURLs {
		for _, issue := range checkSourceURLs(docs) {
			add(KindDeadSource, SeverityWarn, issue, nil, nil)
		}
	}
	if _, err := validateVault(root, true); err != nil {
		for _, issue := range strings.Split(err.Error(), "; ") {
			if strings.Contains(issue, "capture") || strings.Contains(issue, "unregistered raw") {
				add(KindVaultIntegrity, SeverityWarn, issue, nil, nil)
			}
		}
	}

	// --- model-tier candidates --------------------------------------------
	// Only pages whose content changed since the last scan seed new pairs, and
	// each seed considers a handful of lexical neighbours rather than every
	// other page. That is what turns an O(n^2) sweep into work proportional to
	// churn.
	changed := map[string]bool{}
	for id, digest := range digests {
		if state.Pages[id] != digest {
			changed[id] = true
		}
	}
	byID := map[string]Document{}
	for _, d := range docs {
		byID[d.ID] = d
	}
	for id := range changed {
		for _, n := range nearestPages(vectors, id, contradictionNeighbor) {
			other, ok := byID[n.ID]
			if !ok {
				continue
			}
			pair := []string{id, other.ID}
			sort.Strings(pair)
			pairDigests := digestsFor(digests, pair)

			kind := KindContradiction
			detail := fmt.Sprintf("lexically similar (%.2f); check for conflicting claims", n.Similarity)
			if n.Similarity >= nearDuplicateFloor {
				kind = KindNearDuplicate
				detail = fmt.Sprintf("very similar (%.2f); consider merging", n.Similarity)
			} else if n.Similarity < contradictionFloor {
				continue
			}
			if cached(kind, pair, pairDigests) {
				continue
			}
			add(kind, SeverityInfo, detail, pair, pairDigests)
		}
		d := byID[id]
		if strings.TrimSpace(d.Description) != "" && !cached(KindDescriptionDrift, []string{id}, []string{digests[id]}) {
			add(KindDescriptionDrift, SeverityInfo, "page changed; confirm the description still matches the body",
				[]string{id}, []string{digests[id]})
		}
	}

	// --- reconcile ---------------------------------------------------------
	for id, f := range found {
		if existing, ok := state.Findings[id]; ok {
			if existing.Status == StatusResolved || existing.Status == StatusDismissed {
				// A closed finding whose content is unchanged stays closed.
				if sameDigests(existing.Digests, f.Digests) {
					continue
				}
			}
			existing.Detail = f.Detail
			existing.Digests = f.Digests
			if existing.Status != StatusDispatched {
				existing.Status = StatusOpen
			}
			continue
		}
		state.Findings[id] = f
		report.Opened++
	}
	// Deterministic findings are re-derived on every scan, so one that is no
	// longer produced has genuinely been fixed. Model-tier findings are seeded
	// only by *changed* pages, so an idempotent rescan will not reproduce them;
	// they must survive until resolved, or the queue would be destroyed before
	// it could be drained. They are dropped only when a page they refer to is
	// gone.
	live := map[string]bool{}
	for _, d := range docs {
		live[d.ID] = true
	}
	for id, f := range state.Findings {
		if _, still := found[id]; still {
			continue
		}
		if f.Status == StatusResolved || f.Status == StatusDismissed {
			continue
		}
		if f.NeedsModel && conceptsStillPresent(live, f.Concepts) {
			continue
		}
		delete(state.Findings, id)
		report.Closed++
	}

	state.Pages = digests
	state.LastScan = opts.Now.Format(time.RFC3339)
	if err := saveMaintainState(root, state); err != nil {
		return nil, err
	}

	report.Findings = openFindings(state, "", 0)
	for _, f := range report.Findings {
		report.OpenTotal++
		if f.NeedsModel {
			report.ModelQueued++
		}
	}
	return report, nil
}

func conceptsStillPresent(live map[string]bool, concepts []string) bool {
	for _, id := range concepts {
		if !live[id] {
			return false
		}
	}
	return len(concepts) > 0
}

func digestsFor(all map[string]string, ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, all[id])
	}
	return out
}

func sameDigests(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

func openFindings(s *MaintainState, kind string, limit int) []*Finding {
	var out []*Finding
	for _, f := range s.Findings {
		if f.Status != StatusOpen && f.Status != StatusDispatched {
			continue
		}
		if kind != "" && f.Kind != kind {
			continue
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return out[i].Severity == SeverityWarn
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].ID < out[j].ID
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func stripMarkdown(s string) string {
	return markdownNoise.ReplaceAllString(s, " ")
}

func checkSourceURLs(docs []Document) []string {
	client := &http.Client{Timeout: 10 * time.Second}
	seen := map[string]bool{}
	var issues []string
	for _, d := range docs {
		for _, src := range d.Sources {
			if !strings.HasPrefix(src.Resource, "http://") && !strings.HasPrefix(src.Resource, "https://") {
				continue
			}
			if seen[src.Resource] {
				continue
			}
			seen[src.Resource] = true
			req, err := http.NewRequest(http.MethodHead, src.Resource, nil)
			if err != nil {
				continue
			}
			res, err := client.Do(req)
			if err != nil {
				issues = append(issues, fmt.Sprintf("%s: source unreachable %s: %v", d.Path, src.Resource, err))
				continue
			}
			_ = res.Body.Close()
			if res.StatusCode >= 400 {
				issues = append(issues, fmt.Sprintf("%s: source %s returned HTTP %d", d.Path, src.Resource, res.StatusCode))
			}
		}
	}
	return issues
}

// --- work orders ----------------------------------------------------------

// WorkOrderPassage is one excerpt handed to the model tier.
type WorkOrderPassage struct {
	Concept string `json:"concept"`
	Title   string `json:"title"`
	Heading string `json:"heading,omitempty"`
	Text    string `json:"text"`
}

// WorkOrder is a single unit of model-tier maintenance. It carries excerpts
// rather than whole pages so that triage stays cheap; a harness that needs more
// can always call jot get.
type WorkOrder struct {
	FindingID string             `json:"finding_id"`
	Kind      string             `json:"kind"`
	Question  string             `json:"question"`
	Answers   []string           `json:"allowed_verdicts"`
	Concepts  []string           `json:"concepts"`
	Passages  []WorkOrderPassage `json:"passages"`
}

// WorkOrderBatch is what jot maintain --drain emits.
type WorkOrderBatch struct {
	Revision string      `json:"revision,omitempty"`
	Contract string      `json:"contract"`
	Orders   []WorkOrder `json:"orders"`
}

const workOrderContract = "Answer each order with {\"finding_id\":..., \"verdict\":..., \"note\":...} " +
	"and feed the array back to: jot maintain --resolve --stdin. " +
	"jot never edits the wiki from a verdict; propose changes through jot apply."

var verdictOptions = map[string][]string{
	KindContradiction:    {"conflict", "no-conflict", "needs-human"},
	KindNearDuplicate:    {"merge", "distinct", "needs-human"},
	KindDescriptionDrift: {"accurate", "drifted", "needs-human"},
}

var verdictQuestions = map[string]string{
	KindContradiction:    "Do these passages make claims that contradict each other?",
	KindNearDuplicate:    "Do these two pages cover the same concept closely enough to merge?",
	KindDescriptionDrift: "Does the description still accurately summarise this page's body?",
}

// drainWorkOrders selects up to limit model-tier findings and marks them
// dispatched. Excerpts are chosen by overlap with the other page in the pair.
func drainWorkOrders(ctx context.Context, root string, limit int, kind string) (*WorkOrderBatch, error) {
	state, err := loadMaintainState(root)
	if err != nil {
		return nil, err
	}
	chunks, err := buildCorpus(root, false)
	if err != nil {
		return nil, err
	}
	vectors := pageVectors(chunks)
	byPage := map[string][]Chunk{}
	for _, c := range chunks {
		byPage[c.ID] = append(byPage[c.ID], c)
	}

	batch := &WorkOrderBatch{Contract: workOrderContract, Revision: currentRevision(ctx, root), Orders: []WorkOrder{}}
	var picked []*Finding
	for _, f := range openFindings(state, kind, 0) {
		if !f.NeedsModel || f.Status == StatusDispatched {
			continue
		}
		picked = append(picked, f)
		if limit > 0 && len(picked) >= limit {
			break
		}
	}
	for _, f := range picked {
		order := WorkOrder{
			FindingID: f.ID,
			Kind:      f.Kind,
			Question:  verdictQuestions[f.Kind],
			Answers:   verdictOptions[f.Kind],
			Concepts:  f.Concepts,
		}
		for _, id := range f.Concepts {
			var against map[string]int
			for _, other := range f.Concepts {
				if other != id {
					against = vectors[other]
					break
				}
			}
			for _, c := range topChunks(byPage[id], against, 2) {
				order.Passages = append(order.Passages, WorkOrderPassage{
					Concept: id, Title: c.Title, Heading: c.Heading, Text: c.Excerpt,
				})
			}
		}
		batch.Orders = append(batch.Orders, order)
		f.Status = StatusDispatched
	}
	if len(batch.Orders) > 0 {
		if err := saveMaintainState(root, state); err != nil {
			return nil, err
		}
	}
	return batch, nil
}

// topChunks picks the n passages of a page that overlap most with against.
// With no comparison vector it falls back to the page's leading passages.
func topChunks(chunks []Chunk, against map[string]int, n int) []Chunk {
	if len(chunks) == 0 {
		return nil
	}
	out := append([]Chunk(nil), chunks...)
	if against != nil {
		sort.SliceStable(out, func(i, j int) bool {
			return cosine(out[i].tf, against) > cosine(out[j].tf, against)
		})
	}
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// Resolution is one verdict returned by the model tier.
type Resolution struct {
	FindingID string `json:"finding_id"`
	Verdict   string `json:"verdict"`
	By        string `json:"by,omitempty"`
	Note      string `json:"note,omitempty"`
}

// ResolveReport summarises an ingest of verdicts.
type ResolveReport struct {
	Applied int      `json:"applied"`
	Unknown []string `json:"unknown,omitempty"`
	Cached  int      `json:"cached"`
}

// resolveFindings records verdicts and caches them against the exact content
// they judged, so an unchanged pair is never queued again.
func resolveFindings(root string, resolutions []Resolution) (*ResolveReport, error) {
	state, err := loadMaintainState(root)
	if err != nil {
		return nil, err
	}
	report := &ResolveReport{}
	now := isoNow()
	for _, r := range resolutions {
		f, ok := state.Findings[r.FindingID]
		if !ok {
			report.Unknown = append(report.Unknown, r.FindingID)
			continue
		}
		if strings.TrimSpace(r.Verdict) == "" {
			return nil, fmt.Errorf("finding %s: verdict is required", r.FindingID)
		}
		if allowed, ok := verdictOptions[f.Kind]; ok && !contains(allowed, r.Verdict) {
			return nil, fmt.Errorf("finding %s: verdict must be one of %s", r.FindingID, strings.Join(allowed, ", "))
		}
		f.Verdict = r.Verdict
		f.Note = r.Note
		f.VerdictBy = r.By
		f.VerdictAt = now
		f.Status = StatusResolved
		report.Applied++
		if len(f.Digests) > 0 {
			state.Verdicts[verdictKey(f.Kind, f.Digests...)] = VerdictRecord{
				Verdict: r.Verdict, By: r.By, At: now, Note: r.Note,
			}
			report.Cached++
		}
	}
	if err := saveMaintainState(root, state); err != nil {
		return nil, err
	}
	return report, nil
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
