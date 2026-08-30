package jot

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

// htmlComment strips marker comments such as <!-- jot:authoritative --> so
// they never surface in an excerpt or pollute the token stream.
var htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)

var markdownNoise = regexp.MustCompile(`\[\[|\]\]|\[[^\]]+\]\([^\)]+\)|[*_~#>` + "`" + `]+`)

// Chunk is one ranked passage. tf and length back BM25 and are never emitted.
type Chunk struct {
	ID        string   `json:"id"`
	Path      string   `json:"path"`
	Title     string   `json:"title"`
	Type      string   `json:"type"`
	Heading   string   `json:"heading,omitempty"`
	Trust     string   `json:"trust,omitempty"`
	Status    string   `json:"status,omitempty"`
	Timestamp string   `json:"timestamp,omitempty"`
	Raw       bool     `json:"raw,omitempty"`
	Score     float64  `json:"score"`
	Excerpt   string   `json:"excerpt"`
	Matched   []string `json:"matched,omitempty"`

	tf     map[string]int
	length int
}

// SearchOptions narrows and shapes a ranked retrieval.
type SearchOptions struct {
	Limit      int
	MaxChars   int
	PerPage    int // maximum chunks from one page; 0 means 2
	Type       string
	PathPrefix string
	Since      time.Time
	IncludeRaw bool
	Full       bool // excerpts carry the whole passage rather than a 500-char window
	// Prefix treats the final query token as a prefix. Type-ahead needs this:
	// while someone is still typing "depl", an exact-token match finds nothing.
	Prefix bool
}

func (o SearchOptions) perPage() int {
	if o.PerPage > 0 {
		return o.PerPage
	}
	return 2
}

func tokenize(s string) []string {
	s = strings.ToLower(markdownNoise.ReplaceAllString(s, " "))
	return strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func termFrequency(tokens []string) map[string]int {
	tf := make(map[string]int, len(tokens))
	for _, tok := range tokens {
		tf[tok]++
	}
	return tf
}

func chunksForDocument(d Document) []Chunk {
	lines := strings.Split(strings.ReplaceAll(d.Body, "\r\n", "\n"), "\n")
	heading := d.Title
	var paragraph []string
	var chunks []Chunk
	inCode := false
	flush := func() {
		text := strings.TrimSpace(htmlComment.ReplaceAllString(strings.Join(paragraph, "\n"), " "))
		paragraph = nil
		if text == "" {
			return
		}
		weighted := d.Title + " " + d.Title + " " + d.Description + " " + d.Type + " " +
			strings.Join(d.Tags, " ") + " " + heading + " " + heading + " " + text
		tokens := tokenize(weighted)
		chunks = append(chunks, Chunk{
			ID: d.ID, Path: d.Path, Title: d.Title, Type: d.Type, Heading: heading,
			Excerpt: excerpt(text, 500), tf: termFrequency(tokens), length: len(tokens),
		})
	}
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			inCode = !inCode
			paragraph = append(paragraph, line)
			continue
		}
		if !inCode && strings.HasPrefix(trim, "#") {
			flush()
			heading = strings.TrimSpace(strings.TrimLeft(trim, "#"))
			continue
		}
		if !inCode && trim == "" {
			flush()
			continue
		}
		paragraph = append(paragraph, line)
	}
	flush()
	if len(chunks) == 0 {
		weighted := d.Title + " " + d.Title + " " + d.Description + " " + d.Type
		tokens := tokenize(weighted)
		chunks = append(chunks, Chunk{
			ID: d.ID, Path: d.Path, Title: d.Title, Type: d.Type,
			Excerpt: d.Description, tf: termFrequency(tokens), length: len(tokens),
		})
	}
	return chunks
}

func excerpt(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if max <= 0 || len(s) <= max {
		return s
	}
	return strings.TrimSpace(s[:max]) + "…"
}

// rankChunks scores a prepared chunk set with BM25.
func rankChunks(all []Chunk, query string, opts SearchOptions) []Chunk {
	q := tokenize(query)
	if len(q) == 0 || opts.Limit <= 0 || len(all) == 0 {
		return nil
	}
	df := map[string]int{}
	avgLen := 0.0
	for _, c := range all {
		avgLen += float64(c.length)
		for tok := range c.tf {
			df[tok]++
		}
	}
	avgLen /= float64(len(all))
	if avgLen == 0 {
		avgLen = 1
	}
	if opts.Prefix {
		q = expandPrefix(q, df)
	}
	const k1, b = 1.5, 0.75
	scored := make([]Chunk, 0, len(all))
	for _, c := range all {
		var score float64
		var matched []string
		for _, tok := range q {
			freq := float64(c.tf[tok])
			if freq == 0 {
				continue
			}
			matched = append(matched, tok)
			idf := math.Log(1 + (float64(len(all)-df[tok])+0.5)/(float64(df[tok])+0.5))
			denom := freq + k1*(1-b+b*float64(c.length)/avgLen)
			score += idf * (freq * (k1 + 1)) / denom
		}
		if score <= 0 {
			continue
		}
		c.Score = math.Round(score*10000) / 10000
		c.Matched = dedupe(matched)
		scored = append(scored, c)
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			if scored[i].Path == scored[j].Path {
				return scored[i].Heading < scored[j].Heading
			}
			return scored[i].Path < scored[j].Path
		}
		return scored[i].Score > scored[j].Score
	})

	result := make([]Chunk, 0, opts.Limit)
	used := 0
	perPage := map[string]int{}
	limitPerPage := opts.perPage()
	for _, c := range scored {
		if len(result) >= opts.Limit {
			break
		}
		if perPage[c.ID] >= limitPerPage {
			continue
		}
		if opts.MaxChars > 0 && used+len(c.Excerpt) > opts.MaxChars {
			continue
		}
		c.tf, c.length = nil, 0
		result = append(result, c)
		used += len(c.Excerpt)
		perPage[c.ID]++
	}
	return result
}

// prefixExpansionLimit caps how many vocabulary terms one prefix may pull in,
// so a single letter cannot turn into a scan of the whole vocabulary.
const prefixExpansionLimit = 40

// expandPrefix rewrites the final query token into every vocabulary term that
// starts with it. The earlier tokens are left alone: only the word still being
// typed should match loosely.
func expandPrefix(q []string, df map[string]int) []string {
	if len(q) == 0 {
		return q
	}
	last := q[len(q)-1]
	if len(last) < 2 {
		return q
	}
	matches := make([]string, 0, prefixExpansionLimit)
	for term := range df {
		if term != last && strings.HasPrefix(term, last) {
			matches = append(matches, term)
		}
	}
	if len(matches) == 0 {
		return q
	}
	// Rarer terms first, then alphabetical, so the cap keeps the most
	// selective expansions and the result stays deterministic.
	sort.Slice(matches, func(i, j int) bool {
		if df[matches[i]] == df[matches[j]] {
			return matches[i] < matches[j]
		}
		return df[matches[i]] < df[matches[j]]
	})
	if len(matches) > prefixExpansionLimit {
		matches = matches[:prefixExpansionLimit]
	}
	return append(append([]string(nil), q...), matches...)
}

func dedupe(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func filterChunks(all []Chunk, opts SearchOptions) []Chunk {
	if opts.Type == "" && opts.PathPrefix == "" && opts.Since.IsZero() && opts.IncludeRaw {
		return all
	}
	out := all[:0:0]
	for _, c := range all {
		if !opts.IncludeRaw && c.Raw {
			continue
		}
		if opts.Type != "" && !strings.EqualFold(c.Type, opts.Type) {
			continue
		}
		if opts.PathPrefix != "" && !strings.HasPrefix(c.ID, strings.TrimSuffix(opts.PathPrefix, "/")) {
			continue
		}
		if !opts.Since.IsZero() {
			t, err := time.Parse(time.RFC3339, c.Timestamp)
			if err != nil || t.Before(opts.Since) {
				continue
			}
		}
		out = append(out, c)
	}
	return out
}

// searchVault is the retrieval entry point used by the CLI and the web UI.
func searchVault(root, query string, opts SearchOptions) ([]Chunk, error) {
	all, err := buildCorpus(root, opts.IncludeRaw)
	if err != nil {
		return nil, err
	}
	hits := rankChunks(filterChunks(all, opts), query, opts)
	if opts.Full {
		hits = expandHits(root, hits)
	}
	return hits, nil
}

// expandHits replaces windowed excerpts with the page's full body.
func expandHits(root string, hits []Chunk) []Chunk {
	bodies := map[string]string{}
	for i, h := range hits {
		body, ok := bodies[h.Path]
		if !ok {
			d, err := readDocument(root, h.Path)
			if err != nil {
				continue
			}
			body = strings.TrimSpace(d.Body)
			bodies[h.Path] = body
		}
		hits[i].Excerpt = body
	}
	return hits
}

// rankDocuments ranks in-memory documents without touching the cache. It backs
// tests and any caller that already holds parsed documents.
func rankDocuments(docs []Document, query string, limit, maxChars int) []Chunk {
	var all []Chunk
	for _, d := range docs {
		all = append(all, chunksForDocument(d)...)
	}
	return rankChunks(all, query, SearchOptions{Limit: limit, MaxChars: maxChars, IncludeRaw: true})
}

// pageVectors folds chunk-level term frequencies up to one vector per page.
func pageVectors(chunks []Chunk) map[string]map[string]int {
	pages := map[string]map[string]int{}
	for _, c := range chunks {
		if c.Raw {
			continue
		}
		vec, ok := pages[c.ID]
		if !ok {
			vec = map[string]int{}
			pages[c.ID] = vec
		}
		for tok, n := range c.tf {
			vec[tok] += n
		}
	}
	return pages
}

func cosine(a, b map[string]int) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	small, large := a, b
	if len(large) < len(small) {
		small, large = large, small
	}
	var dot float64
	for tok, n := range small {
		if m, ok := large[tok]; ok {
			dot += float64(n) * float64(m)
		}
	}
	if dot == 0 {
		return 0
	}
	var na, nb float64
	for _, n := range a {
		na += float64(n) * float64(n)
	}
	for _, n := range b {
		nb += float64(n) * float64(n)
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// Neighbor is one lexically similar page.
type Neighbor struct {
	ID         string  `json:"id"`
	Similarity float64 `json:"similarity"`
}

// nearestPages returns the k pages most similar to id. This is what keeps the
// model tier of jot maintain from being O(n^2): only near neighbours of a
// changed page are ever considered as contradiction candidates.
func nearestPages(vectors map[string]map[string]int, id string, k int) []Neighbor {
	self, ok := vectors[id]
	if !ok {
		return nil
	}
	out := make([]Neighbor, 0, len(vectors))
	for other, vec := range vectors {
		if other == id {
			continue
		}
		if sim := cosine(self, vec); sim > 0 {
			out = append(out, Neighbor{ID: other, Similarity: math.Round(sim*10000) / 10000})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Similarity == out[j].Similarity {
			return out[i].ID < out[j].ID
		}
		return out[i].Similarity > out[j].Similarity
	})
	if len(out) > k {
		out = out[:k]
	}
	return out
}
