//go:build integration

package publishing

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func readGlobalPins(t *testing.T, f *imageFixture) []globalPin {
	t.Helper()
	w := f.call("GET", "/api/v1/admin/pins", "", true)
	var list struct {
		Items []globalPin `json:"items"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil {
		t.Fatalf("pin list: %d %s", w.Code, w.Body)
	}
	return list.Items
}
func pinOrderBody(items []globalPin) string {
	order := []pinOrderItem{}
	for _, pin := range items {
		order = append(order, pinOrderItem{pin.ID, pin.Version})
	}
	body, _ := json.Marshal(map[string]any{"items": order})
	return string(body)
}
func TestGlobalPinOrderAtomicityConcurrencyAndPrivacy(t *testing.T) {
	f := newImageFixture(t)
	for i := 0; i < 4; i++ {
		id, revision := fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1), fmt.Sprintf("10000000-0000-4000-8000-%012d", i+1)
		visibility := "public"
		if i == 1 {
			visibility = "private"
		}
		if _, err := f.db.ExecContext(f.ctx, `INSERT INTO articles(id,slug,visibility,published_at,updated_at) VALUES($1,$2,$3,'2024-01-01','2024-02-01')`, id, fmt.Sprintf("ordered-%d", i), visibility); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.ExecContext(f.ctx, `INSERT INTO revisions(id,article_id,title,language,written_at,markdown) VALUES($1,$2,$3,'en','2023-01-01','body')`, revision, id, fmt.Sprintf("Pin %d", i)); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.ExecContext(f.ctx, `UPDATE articles SET published_revision_id=$1,pinned=$2 WHERE id=$3`, revision, i < 3, id); err != nil {
			t.Fatal(err)
		}
	}
	initial := readGlobalPins(t, f)
	if len(initial) != 3 || initial[1].Visibility != "private" {
		t.Fatal("complete owner pins missing")
	}
	for _, method := range []string{"GET", "PUT"} {
		path := "/api/v1/admin/pins"
		if method == "PUT" {
			path += "/order"
		}
		if w := f.call(method, path, pinOrderBody(initial), false); w.Code != 401 {
			t.Fatal("guest accessed owner pin management")
		}
	}
	for _, header := range []string{"X-CSRF-Token", "Origin"} {
		r := httptest.NewRequest("PUT", "/api/v1/admin/pins/order", strings.NewReader(pinOrderBody(initial)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://blog.example")
		r.Header.Set("X-CSRF-Token", f.csrf)
		r.AddCookie(f.cookie)
		r.Header.Set(header, "invalid")
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("missing boundary %s: %d", header, w.Code)
		}
	}
	for _, body := range []string{`{}`, `{"items":null}`, `{"items":[{}]}`, `{"items":[{"id":"bad","expected_version":1}]}`, `{"items":[],"extra":true}`, fmt.Sprintf(`{"items":[{"id":%q,"expected_version":1},{"id":%q,"expected_version":1}]}`, initial[0].ID, initial[0].ID)} {
		if w := f.call("PUT", "/api/v1/admin/pins/order", body, true); w.Code != 422 {
			t.Fatalf("invalid request %d %s", w.Code, w.Body)
		}
	}
	if w := f.call("PUT", "/api/v1/admin/pins/order", pinOrderBody(initial[:2]), true); w.Code != 409 {
		t.Fatal("omitted pin accepted")
	}
	reversed := []globalPin{initial[2], initial[1], initial[0]}
	if w := f.call("PUT", "/api/v1/admin/pins/order", pinOrderBody(reversed), true); w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	after := readGlobalPins(t, f)
	for i, pin := range after {
		if pin.ID != reversed[i].ID || pin.Version != reversed[i].Version+1 {
			t.Fatal("saved order/version incorrect")
		}
	}
	var preserved int
	if err := f.db.QueryRowContext(f.ctx, `SELECT count(*) FROM articles a JOIN revisions r ON r.id=a.published_revision_id WHERE a.published_at='2024-01-01' AND a.updated_at='2024-02-01' AND r.written_at='2023-01-01' AND r.markdown='body' AND NOT a.category_pinned`).Scan(&preserved); err != nil || preserved != 4 {
		t.Fatal("reorder changed content/dates/category pins", err)
	}
	if w := f.call("PUT", "/api/v1/admin/pins/order", pinOrderBody(initial), true); w.Code != 409 {
		t.Fatal("stale order accepted")
	}
	// Exactly one simultaneous save may use a given set of versions.
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for range 2 {
		wg.Go(func() { codes <- f.call("PUT", "/api/v1/admin/pins/order", pinOrderBody(after), true).Code })
	}
	wg.Wait()
	close(codes)
	success, conflict := 0, 0
	for code := range codes {
		if code == 200 {
			success++
		} else if code == 409 {
			conflict++
		} else {
			t.Fatalf("concurrent save %d", code)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("concurrent orders overwrote one another")
	}
	// A concurrent new pin either precedes the save (conflict) or appends after
	// it (both succeed). It must never be omitted or inserted between saved pins.
	currentOrder := readGlobalPins(t, f)
	newID := "00000000-0000-4000-8000-000000000004"
	var pinCode, orderCode int
	wg.Go(func() {
		pinCode = f.call("PATCH", "/api/v1/admin/articles/"+newID, `{"expected_version":1,"pinned":true}`, true).Code
	})
	wg.Go(func() { orderCode = f.call("PUT", "/api/v1/admin/pins/order", pinOrderBody(currentOrder), true).Code })
	wg.Wait()
	if pinCode != 200 || (orderCode != 200 && orderCode != 409) {
		t.Fatalf("concurrent pin/save: %d/%d", pinCode, orderCode)
	}
	withNew := readGlobalPins(t, f)
	if len(withNew) != 4 || withNew[3].ID != newID {
		t.Fatal("concurrent new pin lost or failed to append")
	}
	imageArticle(t, f.call("PATCH", "/api/v1/admin/articles/"+newID, fmt.Sprintf(`{"expected_version":%d,"pinned":false}`, withNew[3].Version), true))
	current := readGlobalPins(t, f)
	unpin := current[1]
	base := "/api/v1/admin/articles/" + unpin.ID
	a := imageArticle(t, f.call("PATCH", base, fmt.Sprintf(`{"expected_version":%d,"pinned":false}`, unpin.Version), true))
	if w := f.call("PUT", "/api/v1/admin/pins/order", pinOrderBody(current), true); w.Code != 409 {
		t.Fatal("changed membership accepted")
	}
	a = imageArticle(t, f.call("PATCH", base, fmt.Sprintf(`{"expected_version":%d,"pinned":true}`, a.Version), true))
	current = readGlobalPins(t, f)
	if current[len(current)-1].ID != a.ID {
		t.Fatal("repin did not append")
	}
	a = imageArticle(t, f.call("POST", base+"/unpublish", fmt.Sprintf(`{"expected_version":%d}`, a.Version), true))
	if a.Pinned || len(readGlobalPins(t, f)) != 2 {
		t.Fatal("unpublish did not remove pin")
	}
	if w := f.call("GET", "/api/v1/admin/pins?limit=1", "", true); w.Code != 400 {
		t.Fatal("partial pin management list accepted")
	}
}
