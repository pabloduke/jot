package jot

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// A siteSnapshot is everything the web UI needs to render any page: the
// document set, the link graph, similarity vectors, edit times and tag index.
// Building it touches every file, so it is memoised and invalidated by a
// stat-only fingerprint of the wiki directory.
type siteSnapshot struct {
	docs    []Document
	byID    map[string]Document
	titles  map[string]string
	graph   *LinkGraph
	recency *Recency
	vectors map[string]map[string]int
	topics  []string
	byTopic map[string][]Document
	tags    map[string][]string

	fingerprint string
}

type wikiServer struct {
	root string

	mu   sync.Mutex
	snap *siteSnapshot
}

// fingerprintWiki summarises the wiki tree without reading file contents.
func fingerprintWiki(root string) string {
	var b strings.Builder
	wiki := filepath.Join(root, "wiki")
	_ = filepath.WalkDir(wiki, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		b.WriteString(filepath.ToSlash(rel))
		b.WriteByte(0)
		b.WriteString(itoa(int(info.Size())))
		b.WriteByte(0)
		b.WriteString(itoa(int(info.ModTime().UnixNano())))
		b.WriteByte('\n')
		return nil
	})
	return digestBytes([]byte(b.String()))
}

// site returns a current snapshot, rebuilding only when the wiki has changed.
func (s *wikiServer) site(ctx context.Context) (*siteSnapshot, error) {
	fp := fingerprintWiki(s.root)

	s.mu.Lock()
	cached := s.snap
	s.mu.Unlock()
	if cached != nil && cached.fingerprint == fp {
		return cached, nil
	}

	docs, _, err := loadConcepts(s.root, true)
	if err != nil {
		return nil, err
	}
	snap := &siteSnapshot{
		docs:        docs,
		byID:        make(map[string]Document, len(docs)),
		titles:      make(map[string]string, len(docs)),
		byTopic:     map[string][]Document{},
		tags:        map[string][]string{},
		fingerprint: fp,
	}
	for _, d := range docs {
		snap.byID[d.ID] = d
		snap.titles[d.ID] = d.Title
		topic := topicOf(d.ID)
		snap.byTopic[topic] = append(snap.byTopic[topic], d)
		for _, tag := range d.Tags {
			key := strings.ToLower(strings.TrimSpace(tag))
			if key != "" {
				snap.tags[key] = append(snap.tags[key], d.ID)
			}
		}
	}
	for topic := range snap.byTopic {
		snap.topics = append(snap.topics, topic)
	}
	sort.Strings(snap.topics)
	for tag := range snap.tags {
		sort.Strings(snap.tags[tag])
	}

	snap.graph = buildLinkGraph(s.root, docs)
	snap.recency = loadRecency(ctx, s.root)
	if chunks, err := buildCorpus(s.root, false); err == nil {
		snap.vectors = pageVectors(chunks)
	} else {
		snap.vectors = map[string]map[string]int{}
	}

	s.mu.Lock()
	s.snap = snap
	s.mu.Unlock()
	return snap, nil
}

// related returns the most lexically similar pages, excluding ones already
// reachable as a backlink so the block adds something the page does not
// already show.
func (snap *siteSnapshot) related(id string, n int) []Neighbor {
	back := map[string]bool{}
	for _, b := range snap.graph.Backlinks(id) {
		back[b] = true
	}
	out := make([]Neighbor, 0, n)
	for _, cand := range nearestPages(snap.vectors, id, n*3) {
		if back[cand.ID] || cand.Similarity < 0.10 {
			continue
		}
		if _, ok := snap.byID[cand.ID]; !ok {
			continue
		}
		out = append(out, cand)
		if len(out) >= n {
			break
		}
	}
	return out
}

// siblings lists the other pages in the same topic directory.
func (snap *siteSnapshot) siblings(id string) []Document {
	topic := topicOf(id)
	var out []Document
	for _, d := range snap.byTopic[topic] {
		out = append(out, d)
	}
	return out
}

// TagCount is one entry of the tag index.
type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

func (snap *siteSnapshot) tagCounts() []TagCount {
	out := make([]TagCount, 0, len(snap.tags))
	for tag, ids := range snap.tags {
		out = append(out, TagCount{Tag: tag, Count: len(ids)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Tag < out[j].Tag
		}
		return out[i].Count > out[j].Count
	})
	return out
}

// LooseEnd is one item on the rediscovery page.
type LooseEnd struct {
	ID    string
	Title string
	Why   string
	When  time.Time
	Href  string
}

// LooseLane groups loose ends under one heading.
type LooseLane struct {
	Key   string
	Title string
	Blurb string
	Items []LooseEnd
}

const forgottenAfter = 270 * 24 * time.Hour

// looseEnds assembles the rediscovery page. It is deliberately read-only: it
// derives everything from the snapshot and the manifest rather than running
// the maintenance scan, so loading a page never mutates vault state.
func (s *wikiServer) looseEnds(snap *siteSnapshot, now time.Time) []LooseLane {
	href := func(id string) string { return "/wiki/" + pathURL(id) }
	lanes := []LooseLane{
		{Key: "captures", Title: "Captured but never compiled",
			Blurb: "You saved these on purpose and then never turned them into anything."},
		{Key: "cold", Title: "Untouched for a long time",
			Blurb: "Still here, still unedited. Worth a re-read or a delete."},
		{Key: "orphans", Title: "Nothing links here",
			Blurb: "Unreachable except by search, so in practice you never find them."},
		{Key: "stale", Title: "Past their stale-by date",
			Blurb: "These declared a stale_after that has come and gone."},
		{Key: "unverified", Title: "Never verified",
			Blurb: "No verified entry, so nothing has confirmed these against a source."},
		{Key: "thin", Title: "Stubs",
			Blurb: "Short enough that they are probably unfinished."},
		{Key: "conflicts", Title: "Open contradictions",
			Blurb: "Recorded disagreements that were never resolved."},
	}
	byKey := map[string]*LooseLane{}
	for i := range lanes {
		byKey[lanes[i].Key] = &lanes[i]
	}
	add := func(key, id, title, why string, when time.Time) {
		lane := byKey[key]
		if lane == nil {
			return
		}
		lane.Items = append(lane.Items, LooseEnd{ID: id, Title: title, Why: why, When: when, Href: href(id)})
	}

	if captures, err := pendingCaptures(s.root); err == nil {
		for _, c := range captures {
			when := parseTimeOrZero(strings.SplitN(c.CreatedAt, ".", 2)[0] + "Z")
			if t, err := time.Parse(time.RFC3339Nano, c.CreatedAt); err == nil {
				when = t
			}
			lane := byKey["captures"]
			lane.Items = append(lane.Items, LooseEnd{
				ID: c.ID, Title: c.Title, Why: c.SourceKind, When: when,
			})
		}
	}

	orphans := map[string]bool{}
	for _, id := range snap.graph.Orphans(snap.docs) {
		orphans[id] = true
	}
	for _, d := range snap.docs {
		updated := snap.recency.Updated(d.Path)
		if !updated.IsZero() && now.Sub(updated) > forgottenAfter {
			add("cold", d.ID, d.Title, "last edited "+humanAge(updated, now), updated)
		}
		if orphans[d.ID] {
			add("orphans", d.ID, d.Title, d.Description, updated)
		}
		if d.IsStale(now) {
			add("stale", d.ID, d.Title, "stale after "+d.StaleAfter, updated)
		}
		if d.Trust == TrustUnverified {
			add("unverified", d.ID, d.Title, d.EffectiveStatus(), updated)
		}
		if len(strings.Fields(stripMarkdown(d.Body))) < thinPageWords {
			add("thin", d.ID, d.Title, itoa(len(strings.Fields(stripMarkdown(d.Body))))+" words", updated)
		}
		for _, other := range d.Conflicts {
			target := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(other), "/"), ".md")
			add("conflicts", d.ID, d.Title, "contradicts "+target, updated)
		}
	}

	for i := range lanes {
		items := lanes[i].Items
		sort.SliceStable(items, func(a, b int) bool {
			if items[a].When.Equal(items[b].When) {
				return items[a].Title < items[b].Title
			}
			return items[a].When.Before(items[b].When)
		})
		lanes[i].Items = items
	}
	return lanes
}
