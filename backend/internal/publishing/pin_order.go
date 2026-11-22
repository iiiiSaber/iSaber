package publishing

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
)

type globalPin struct {
	ID         string `json:"id"`
	Slug       string `json:"slug"`
	Title      string `json:"title"`
	Visibility string `json:"visibility"`
	Version    int64  `json:"version"`
}

type pinOrderItem struct {
	ID      string `json:"id"`
	Version int64  `json:"expected_version"`
}

// Take this before article row locks for pin/unpin, unpublish and reorder.
// Other article edits lock one row and cannot change list membership.
func lockGlobalPins(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(7311176)`)
	return err
}

func (s *Service) pinOrderRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/admin/pins", s.listGlobalPins)
	mux.HandleFunc("PUT /api/v1/admin/pins/order", s.reorderGlobalPins)
}

func (s *Service) listGlobalPins(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		reply(w, nil, fault(400, "invalid_pin_parameters"))
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT a.id,a.slug,r.title,a.visibility,a.version
 FROM articles a JOIN revisions r ON r.id=a.published_revision_id WHERE a.pinned ORDER BY a.pin_position,a.id`)
	if err != nil {
		reply(w, nil, err)
		return
	}
	defer rows.Close()
	items := []globalPin{}
	for rows.Next() {
		var pin globalPin
		if err := rows.Scan(&pin.ID, &pin.Slug, &pin.Title, &pin.Visibility, &pin.Version); err != nil {
			reply(w, nil, err)
			return
		}
		items = append(items, pin)
	}
	reply(w, map[string]any{"items": items}, rows.Err())
}

func (s *Service) reorderGlobalPins(w http.ResponseWriter, r *http.Request) {
	fields, err := readFields(w, r)
	if err != nil {
		reply(w, nil, err)
		return
	}
	var items []pinOrderItem
	decoder := json.NewDecoder(bytes.NewReader(fields["items"]))
	decoder.DisallowUnknownFields()
	if len(fields) != 1 || decoder.Decode(&items) != nil || items == nil {
		reply(w, nil, fault(422, "invalid_pin_order"))
		return
	}
	seen := map[string]bool{}
	for i := range items {
		id, err := canonicalID(items[i].ID)
		if err != nil || items[i].Version < 1 || seen[id] {
			reply(w, nil, fault(422, "invalid_pin_order"))
			return
		}
		items[i].ID = id
		seen[id] = true
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		reply(w, nil, err)
		return
	}
	defer tx.Rollback()
	if err := lockGlobalPins(r.Context(), tx); err != nil {
		reply(w, nil, err)
		return
	}
	rows, err := tx.QueryContext(r.Context(), `SELECT id,version FROM articles WHERE pinned ORDER BY id FOR UPDATE`)
	if err != nil {
		reply(w, nil, err)
		return
	}
	versions := map[string]int64{}
	for rows.Next() {
		var id string
		var version int64
		if err := rows.Scan(&id, &version); err != nil {
			rows.Close()
			reply(w, nil, err)
			return
		}
		versions[id] = version
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		reply(w, nil, err)
		return
	}
	if len(items) != len(versions) {
		reply(w, nil, fault(409, "pin_order_conflict"))
		return
	}
	for _, item := range items {
		if versions[item.ID] != item.Version {
			reply(w, nil, fault(409, "pin_order_conflict"))
			return
		}
	}
	// Fresh increasing positions avoid transient unique-index collisions and
	// keep later pins appended. Never change content/publication timestamps.
	for _, item := range items {
		if _, err := tx.ExecContext(r.Context(), `UPDATE articles SET pin_position=nextval('article_pin_position_seq'),version=version+1,modified_at=clock_timestamp() WHERE id=$1`, item.ID); err != nil {
			reply(w, nil, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		reply(w, nil, err)
		return
	}
	reply(w, map[string]string{"status": "reordered"}, nil)
}
