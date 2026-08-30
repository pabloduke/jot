package jot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func get(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))
	return res
}

func mustGet(t *testing.T, handler http.Handler, path string) string {
	t.Helper()
	res := get(t, handler, path)
	if res.Code != http.StatusOK {
		t.Fatalf("%s status = %d: %s", path, res.Code, res.Body.String())
	}
	return res.Body.String()
}

// navVault builds a small vault with two topics, links, and tags.
func navVault(t *testing.T) string {
	t.Helper()
	root := testVault(t)
	tagged := func(id, title, tags, body string) {
		page := "---\ntype: Concept\ntitle: " + title +
			"\ndescription: About " + title +
			"\ntags: [" + tags + "]" +
			"\ngenerated:\n  by: process:jot\n  at: 2026-08-01T00:00:00Z\n---\n\n# " + title +
			"\n\n" + body + "\n\n## A Section\n\nMore detail here about the topic at hand.\n" +
			"\n## Another Section\n\nAnd a second heading so the page has a real outline.\n"
		if err := atomicWrite(filepath.Join(root, "wiki", filepath.FromSlash(id)+".md"), []byte(page), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tagged("work/releases", "Releases", "process, ship", "Deploys go out Tuesday. See [[work/rollback]]. "+longBody)
	tagged("work/rollback", "Rollback", "process", "Revert the artifact. "+longBody)
	tagged("food/bread", "Bread", "baking", "Feed the sourdough starter with flour and water each morning without fail always.")
	return root
}

func TestSidebarShowsSiblingsTOCAndTree(t *testing.T) {
	root := navVault(t)
	body := mustGet(t, newWikiHandler(root), "/wiki/work/releases")

	for _, want := range []string{
		`<aside>`,
		"Rollback",       // sibling in the same topic
		"On this page",   // page TOC
		"#a-section",     // TOC anchor
		`id="a-section"`, // matching heading anchor
		"All topics",     // collapsed full tree
		`id="recently-viewed"`,
		`aria-current="page"`, // the current page marked in the rail
	} {
		if !strings.Contains(body, want) {
			t.Errorf("sidebar missing %q", want)
		}
	}
}

func TestPageShowsBreadcrumbsRelatedAndAge(t *testing.T) {
	root := navVault(t)
	body := mustGet(t, newWikiHandler(root), "/wiki/work/releases")

	if !strings.Contains(body, `class="crumbs"`) {
		t.Error("breadcrumbs missing")
	}
	if !strings.Contains(body, "See also") {
		t.Error("related block missing")
	}
	if !strings.Contains(body, "/wiki/work/rollback") {
		t.Error("expected the sibling to surface as related or linked")
	}
	if !strings.Contains(body, "edited ") {
		t.Error("age chip missing")
	}
	if !strings.Contains(body, `href="/tags/process"`) {
		t.Error("tag chips missing from page meta")
	}
}

func TestRelatedExcludesBacklinkedPages(t *testing.T) {
	root := navVault(t)
	s := &wikiServer{root: root}
	snap, err := s.site(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// rollback is linked *from* releases, so releases is a backlink of
	// rollback and must not be repeated under See also.
	for _, n := range snap.related("work/rollback", 5) {
		if n.ID == "work/releases" {
			t.Fatal("See also repeated a page already shown as a backlink")
		}
	}
}

func TestRecentPage(t *testing.T) {
	root := navVault(t)
	body := mustGet(t, newWikiHandler(root), "/recent")
	for _, want := range []string{"Recently updated", "Longest untouched", "Releases", "Bread"} {
		if !strings.Contains(body, want) {
			t.Errorf("recent page missing %q", want)
		}
	}
}

func TestLooseEndsSurfacesForgottenThings(t *testing.T) {
	root := navVault(t)
	// An uncompiled capture is the truest "you forgot this" signal.
	if _, err := addCapture(root, "Unfinished thought", "message", "", "Something I meant to write up.", nil); err != nil {
		t.Fatal(err)
	}
	// A stub, and a page that has gone stale.
	stale := "---\ntype: Concept\ntitle: Expired\ndescription: Old\n" +
		"stale_after: 2000-01-01T00:00:00Z\ngenerated:\n  by: process:jot\n  at: 2026-08-01T00:00:00Z\n---\n\n# Expired\n\nShort.\n"
	if err := atomicWrite(filepath.Join(root, "wiki", "expired.md"), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	body := mustGet(t, newWikiHandler(root), "/loose-ends")
	for _, want := range []string{
		"Loose ends",
		"Captured but never compiled", "Unfinished thought",
		"Past their stale-by date", "Expired",
		"Nothing links here",
		"Stubs",
		"Never verified",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("loose ends missing %q", want)
		}
	}
}

func TestLooseEndsDoesNotMutateVaultState(t *testing.T) {
	root := navVault(t)
	handler := newWikiHandler(root)
	mustGet(t, handler, "/loose-ends")
	if _, err := os.Stat(maintainPath(root)); !os.IsNotExist(err) {
		t.Fatal("rendering loose ends must not write maintenance state")
	}
}

func TestLooseEndsFlagsLongUntouchedPages(t *testing.T) {
	root := navVault(t)
	old := time.Now().Add(-400 * 24 * time.Hour)
	target := filepath.Join(root, "wiki", "food", "bread.md")
	if err := os.Chtimes(target, old, old); err != nil {
		t.Fatal(err)
	}
	body := mustGet(t, newWikiHandler(root), "/loose-ends")
	if !strings.Contains(body, "Untouched for a long time") || !strings.Contains(body, "Bread") {
		t.Fatalf("stale-by-age lane missing:\n%s", body)
	}
}

func TestTagIndexAndTagPage(t *testing.T) {
	root := navVault(t)
	handler := newWikiHandler(root)

	index := mustGet(t, handler, "/tags")
	for _, want := range []string{"process", "baking", "Topics"} {
		if !strings.Contains(index, want) {
			t.Errorf("tag index missing %q", want)
		}
	}

	page := mustGet(t, handler, "/tags/process")
	if !strings.Contains(page, "Releases") || !strings.Contains(page, "Rollback") {
		t.Errorf("tag page missing its members:\n%s", page)
	}
	// The collapsed "All topics" tree links every page, so assert on the card
	// markup that only the tag's own members produce.
	if strings.Contains(page, "<strong>Bread</strong>") {
		t.Error("tag page listed a page without that tag")
	}

	empty := mustGet(t, handler, "/tags/nonexistent")
	if !strings.Contains(empty, "Nothing carries this tag") {
		t.Error("unknown tag should render an empty state, not an error")
	}
}

func TestGraphRendersLinkedNodes(t *testing.T) {
	root := navVault(t)
	body := mustGet(t, newWikiHandler(root), "/graph")
	for _, want := range []string{"<svg", `class="graph"`, `class="edge"`, "/wiki/work/releases", "Releases"} {
		if !strings.Contains(body, want) {
			t.Errorf("graph missing %q", want)
		}
	}
}

func TestGraphHandlesEmptyVault(t *testing.T) {
	root := testVault(t)
	body := mustGet(t, newWikiHandler(root), "/graph")
	if !strings.Contains(body, "No compiled pages to graph") {
		t.Errorf("empty graph should render an empty state:\n%s", body)
	}
}

func TestRandomRedirectsToAPage(t *testing.T) {
	root := navVault(t)
	res := get(t, newWikiHandler(root), "/random")
	if res.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", res.Code)
	}
	if loc := res.Header().Get("Location"); !strings.HasPrefix(loc, "/wiki/") {
		t.Fatalf("Location = %q", loc)
	}
}

func TestRandomOnEmptyVaultGoesHome(t *testing.T) {
	root := testVault(t)
	res := get(t, newWikiHandler(root), "/random")
	if res.Code != http.StatusSeeOther || res.Header().Get("Location") != "/" {
		t.Fatalf("empty vault random: %d %q", res.Code, res.Header().Get("Location"))
	}
}

func TestTypeAheadEndpoint(t *testing.T) {
	root := navVault(t)
	res := get(t, newWikiHandler(root), "/api/search?q=tuesday&limit=5")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d", res.Code)
	}
	if ct := res.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("content type = %q", ct)
	}
	var payload struct {
		Hits []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Hits) == 0 || payload.Hits[0].ID == "" {
		t.Fatalf("expected suggestions: %s", res.Body.String())
	}

	empty := get(t, newWikiHandler(root), "/api/search?q=")
	if !strings.Contains(empty.Body.String(), `"hits"`) {
		t.Errorf("empty query should still return a hits array: %s", empty.Body.String())
	}
}

func TestSearchFiltersNarrowResults(t *testing.T) {
	root := navVault(t)
	handler := newWikiHandler(root)

	all := mustGet(t, handler, "/search?q=starter+deploy")
	if !strings.Contains(all, "Bread") {
		t.Fatalf("expected an unfiltered match on Bread:\n%s", all)
	}
	filtered := mustGet(t, handler, "/search?q=starter+deploy&path=work")
	if strings.Contains(filtered, `<h2><a href="/wiki/food/bread">`) {
		t.Error("topic filter did not exclude the other topic")
	}
	if !strings.Contains(filtered, `class="filters"`) {
		t.Error("filter form missing from the search page")
	}
}

func TestConnectSrcAllowsTypeAhead(t *testing.T) {
	root := navVault(t)
	res := get(t, newWikiHandler(root), "/")
	csp := res.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "connect-src 'self'") {
		t.Errorf("CSP must permit the type-ahead fetch: %q", csp)
	}
	if strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "unsafe-eval") {
		t.Errorf("CSP must stay strict: %q", csp)
	}
}

func TestKeyboardHelpAndNavPresent(t *testing.T) {
	root := navVault(t)
	body := mustGet(t, newWikiHandler(root), "/")
	for _, want := range []string{`id="help"`, "<kbd>", `href="/recent"`, `href="/loose-ends"`, `href="/tags"`, `href="/graph"`} {
		if !strings.Contains(body, want) {
			t.Errorf("shell missing %q", want)
		}
	}
}

func TestSnapshotIsCachedUntilTheWikiChanges(t *testing.T) {
	root := navVault(t)
	s := &wikiServer{root: root}
	first, err := s.site(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.site(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("an unchanged wiki should reuse the cached snapshot")
	}

	writeConcept(t, root, "work/new-page", "New Page", longBody)
	third, err := s.site(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("snapshot was not invalidated after a page was added")
	}
	if _, ok := third.byID["work/new-page"]; !ok {
		t.Fatal("rebuilt snapshot is missing the new page")
	}
}

func TestHumanAge(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{30 * time.Second, "just now"},
		{5 * time.Minute, "5 minutes ago"},
		{3 * time.Hour, "3 hours ago"},
		{5 * 24 * time.Hour, "5 days ago"},
		{70 * 24 * time.Hour, "2 months ago"},
		{800 * 24 * time.Hour, "2 years ago"},
	}
	for _, tc := range cases {
		if got := humanAge(now.Add(-tc.ago), now); got != tc.want {
			t.Errorf("humanAge(-%s) = %q, want %q", tc.ago, got, tc.want)
		}
	}
	if got := humanAge(time.Time{}, now); got != "unknown" {
		t.Errorf("zero time = %q", got)
	}
}

// A dropdown that only matches whole tokens is useless while someone is still
// typing, so the final query term is expanded as a prefix.
func TestTypeAheadMatchesPartialWords(t *testing.T) {
	root := navVault(t)
	handler := newWikiHandler(root)

	partial := mustGet(t, handler, "/api/search?q=depl")
	if !strings.Contains(partial, "Releases") {
		t.Fatalf("prefix search should match \"deploys\" from \"depl\": %s", partial)
	}

	// Full-page search stays exact so results do not drift unexpectedly.
	hits, err := searchVault(root, "depl", SearchOptions{Limit: 5, MaxChars: 2000})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Errorf("non-prefix search should not match a partial word: %+v", hits)
	}
}

func TestPrefixExpansionIgnoresEarlierTokens(t *testing.T) {
	root := navVault(t)
	// "sourdo" is a prefix of a word only on the bread page; "releases" is an
	// exact token only on the releases page. Expanding only the last token
	// means this finds bread, not releases.
	hits, err := searchVault(root, "sourdo", SearchOptions{Limit: 5, MaxChars: 2000, Prefix: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].ID != "food/bread" {
		t.Fatalf("prefix search missed the intended page: %+v", hits)
	}
}

// Topic chips must land on something that always has content, not a search
// that can legitimately come back empty.
func TestTopicChipsJumpToTheIndexSection(t *testing.T) {
	root := navVault(t)
	handler := newWikiHandler(root)

	tags := mustGet(t, handler, "/tags")
	if !strings.Contains(tags, `href="/#topic-work"`) {
		t.Fatalf("topic chip should anchor into the index:\n%s", tags)
	}
	home := mustGet(t, handler, "/")
	if !strings.Contains(home, `id="topic-work"`) {
		t.Fatalf("index is missing the anchor the chip points at:\n%s", home)
	}
}
