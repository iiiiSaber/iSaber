//go:build integration

package publishing

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func seedFeedEntry(t *testing.T, f *imageFixture, n int, visibility string, published bool, category *string) string {
	t.Helper()
	id, revision := fmt.Sprintf("00000000-0000-4000-8000-%012d", n), fmt.Sprintf("10000000-0000-4000-8000-%012d", n)
	date := time.Date(2026, 1, n, 0, 0, 0, 0, time.UTC)
	if n == 25 {
		date = time.Date(2026, 1, 24, 0, 0, 0, 0, time.UTC)
	}
	if _, err := f.db.ExecContext(f.ctx, `INSERT INTO articles(id,slug,visibility,published_at,updated_at) VALUES($1,$2,$3,$4,$4)`, id, fmt.Sprintf("feed-entry-%d", n), visibility, date); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(f.ctx, `INSERT INTO revisions(id,article_id,title,summary,language,written_at,markdown,category_id) VALUES($1,$2,$3,$4,'en','2026-01-01','body-never-in-feed',$5)`, revision, id, fmt.Sprintf("Public entry %d 中文", n), fmt.Sprintf("Public summary %d", n), category); err != nil {
		t.Fatal(err)
	}
	pointer := "published_revision_id"
	if !published {
		pointer = "draft_revision_id"
	}
	if _, err := f.db.ExecContext(f.ctx, `UPDATE articles SET `+pointer+`=$1 WHERE id=$2`, revision, id); err != nil {
		t.Fatal(err)
	}
	return id
}
func feedCall(f *imageFixture, method, etag string, owner bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v1/feed.xml?q=private&limit=1000&visibility=private", nil)
	req.Host = "untrusted.invalid"
	req.Header.Set("X-Forwarded-Host", "untrusted.invalid")
	req.Header.Set("If-None-Match", etag)
	if owner {
		req.AddCookie(f.cookie)
		req.Header.Set("Authorization", "Bearer ignored")
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, req)
	return w
}
func TestFeedPublicSelectionAndPinIndependence(t *testing.T) {
	f := newImageFixture(t)
	publicCategory := createdLabel(t, f.call("POST", "/api/v1/admin/categories", `{"name":"Public category","slug":"public-category"}`, true))
	privateCategory := createdLabel(t, f.call("POST", "/api/v1/admin/categories", `{"name":"Private category marker","slug":"private-category-marker"}`, true))
	for n := 1; n <= 25; n++ {
		seedFeedEntry(t, f, n, "public", true, &publicCategory.ID)
	}
	privateID := seedFeedEntry(t, f, 26, "private", true, &privateCategory.ID)
	draftID := seedFeedEntry(t, f, 27, "public", false, &privateCategory.ID)
	w := feedCall(f, "GET", "", false)
	if w.Code != 200 || w.Header().Get("Cache-Control") != feedCacheControl || w.Header().Get("Set-Cookie") != "" {
		t.Fatalf("feed: %d %s", w.Code, w.Body)
	}
	parsed := parseFeed(t, w.Body.Bytes())
	items := parsed.Channel.Items
	if len(items) != 20 || items[0].GUID.Value != "urn:uuid:00000000-0000-4000-8000-000000000025" || items[1].GUID.Value != "urn:uuid:00000000-0000-4000-8000-000000000024" || items[19].GUID.Value != "urn:uuid:00000000-0000-4000-8000-000000000006" {
		t.Fatal("feed selection/order/limit failed")
	}
	for _, marker := range []string{privateID, draftID, "Private category marker", "body-never-in-feed", "untrusted.invalid"} {
		if strings.Contains(w.Body.String(), marker) {
			t.Fatalf("feed leaked %q", marker)
		}
	}
	owner := feedCall(f, "GET", "", true)
	if !bytes.Equal(w.Body.Bytes(), owner.Body.Bytes()) || owner.Header().Get("ETag") != w.Header().Get("ETag") {
		t.Fatal("feed varies for owner")
	}
	// An old pinned article must not enter the recent window or alter its validator.
	if _, err := f.db.ExecContext(f.ctx, `UPDATE articles SET pinned=true WHERE slug IN ('feed-entry-1','feed-entry-6')`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(f.ctx, `UPDATE articles SET pin_position=100 WHERE slug='feed-entry-1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(f.ctx, `UPDATE articles SET category_pinned=true WHERE slug='feed-entry-6'`); err != nil {
		t.Fatal(err)
	}
	pinned := feedCall(f, "GET", w.Header().Get("ETag"), false)
	if pinned.Code != 304 || pinned.Body.Len() != 0 {
		t.Fatal("pin state changed feed")
	}
	head := feedCall(f, "HEAD", "", false)
	if head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("ETag") != w.Header().Get("ETag") {
		t.Fatal("HEAD differs")
	}
	conditionalHead := feedCall(f, "HEAD", w.Header().Get("ETag"), false)
	if conditionalHead.Code != 304 || conditionalHead.Body.Len() != 0 {
		t.Fatal("conditional HEAD failed")
	}
	for _, method := range []string{"POST", "PATCH", "PUT", "DELETE", "OPTIONS"} {
		bad := feedCall(f, method, "", false)
		if bad.Code != 405 || bad.Header().Get("Allow") != "GET, HEAD" {
			t.Fatalf("%s method accepted: %d %s", method, bad.Code, bad.Header().Get("Allow"))
		}
	}
	// An old validator must not retain an article after a visibility change.
	if _, err := f.db.ExecContext(f.ctx, `UPDATE articles SET visibility='private' WHERE slug='feed-entry-25'`); err != nil {
		t.Fatal(err)
	}
	removed := feedCall(f, "GET", w.Header().Get("ETag"), true)
	if removed.Code != 200 || removed.Header().Get("ETag") == w.Header().Get("ETag") || strings.Contains(removed.Body.String(), items[0].GUID.Value) || len(parseFeed(t, removed.Body.Bytes()).Channel.Items) != 20 {
		t.Fatal("conditional response retained private entry")
	}
	// Private-only content cannot influence either bytes or the public validator.
	if _, err := f.db.ExecContext(f.ctx, `UPDATE revisions SET summary='private changed' WHERE article_id=$1`, privateID); err != nil {
		t.Fatal(err)
	}
	unchanged := feedCall(f, "GET", removed.Header().Get("ETag"), false)
	if unchanged.Code != 304 {
		t.Fatal("private edit changed public validator")
	}
}
func TestFeedPublicationLifecycleAndDraftIsolation(t *testing.T) {
	f := newImageFixture(t)
	empty := feedCall(f, "GET", "", false)
	if empty.Code != 200 || len(parseFeed(t, empty.Body.Bytes()).Channel.Items) != 0 {
		t.Fatal("empty feed failed")
	}
	job := f.upload(t, "rss.md", []byte("---\nwritten_at: 2024-01-01\nlanguage: en\nsummary: First public summary\n---\n# Feed lifecycle\nBody never syndicated."), nil)
	base := "/api/v1/admin/articles/" + *job.ArticleID
	a := imageArticle(t, f.call("GET", base, "", true))
	a = imageArticle(t, f.call("POST", base+"/publish", fmt.Sprintf(`{"expected_version":%d}`, a.Version), true))
	if feedCall(f, "GET", empty.Header().Get("ETag"), true).Code != 304 {
		t.Fatal("private publication changed feed")
	}
	a = imageArticle(t, f.call("PATCH", base, fmt.Sprintf(`{"expected_version":%d,"visibility":"public"}`, a.Version), true))
	first := feedCall(f, "GET", "", false)
	firstItem := parseFeed(t, first.Body.Bytes()).Channel.Items[0]
	f.upload(t, "changed.md", []byte("---\nwritten_at: 2024-01-01\nlanguage: en\nsummary: Changed public summary\n---\n# Changed public title\nChanged body."), a)
	if feedCall(f, "GET", first.Header().Get("ETag"), true).Code != 304 {
		t.Fatal("draft edit changed public feed")
	}
	a = imageArticle(t, f.call("GET", base, "", true))
	a = imageArticle(t, f.call("POST", base+"/publish", fmt.Sprintf(`{"expected_version":%d}`, a.Version), true))
	changed := feedCall(f, "GET", first.Header().Get("ETag"), false)
	changedItem := parseFeed(t, changed.Body.Bytes()).Channel.Items[0]
	if changed.Code != 200 || changedItem.Title != "Changed public title" || changedItem.Description != "Changed public summary" || changedItem.GUID != firstItem.GUID || changedItem.PubDate != firstItem.PubDate {
		t.Fatal("published edit identity/date mismatch")
	}
	// Markdown is never an implicit summary, even when the stored summary is empty.
	if _, err := f.db.ExecContext(f.ctx, `UPDATE revisions SET summary=NULL WHERE id=$1`, a.Published.ID); err != nil {
		t.Fatal(err)
	}
	blank := feedCall(f, "GET", "", false)
	if parseFeed(t, blank.Body.Bytes()).Channel.Items[0].Description != "" || strings.Contains(blank.Body.String(), "Changed body") {
		t.Fatal("body used as summary")
	}
	a = imageArticle(t, f.call("POST", base+"/unpublish", fmt.Sprintf(`{"expected_version":%d}`, a.Version), true))
	gone := feedCall(f, "GET", blank.Header().Get("ETag"), false)
	if gone.Code != 200 || len(parseFeed(t, gone.Body.Bytes()).Channel.Items) != 0 {
		t.Fatal("unpublish retained feed item")
	}
	a = imageArticle(t, f.call("POST", base+"/publish", fmt.Sprintf(`{"expected_version":%d}`, a.Version), true))
	restored := feedCall(f, "GET", "", false)
	restoredItem := parseFeed(t, restored.Body.Bytes()).Channel.Items[0]
	if restoredItem.GUID != firstItem.GUID || restoredItem.PubDate != firstItem.PubDate {
		t.Fatal("republishing changed stable identity/date")
	}
}
func TestFeedOutageDoesNotReturnCachedOrEmptySuccess(t *testing.T) {
	f := newImageFixture(t)
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		w := feedCall(f, method, `*`, true)
		if w.Code != 503 || w.Header().Get("Retry-After") != "60" || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("ETag") != "" {
			t.Fatalf("outage returned %d: %s", w.Code, w.Body)
		}
		if method == http.MethodHead && w.Body.Len() != 0 {
			t.Fatal("HEAD error has a body")
		}
	}
}
