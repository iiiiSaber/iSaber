//go:build integration

package migrations

import (
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestGlobalPinOrderUpgradeAndLegacyPinWrites(t *testing.T) {
	db, ctx := isolatedDatabase(t)
	provider, err := Provider(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 10); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for i := 0; i < 3; i++ {
		id, revision := fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1), fmt.Sprintf("10000000-0000-4000-8000-%012d", i+1)
		ids = append(ids, id)
		mustExec(t, db, ctx, `INSERT INTO articles(id,slug,visibility,published_at,updated_at) VALUES($1,$2,'public','2025-01-01','2025-02-01')`, id, fmt.Sprintf("existing-%d", i))
		mustExec(t, db, ctx, `INSERT INTO revisions(id,article_id,title,language,written_at,markdown) VALUES($1,$2,'Existing writing','en',$3,'Unchanged body')`, revision, id, fmt.Sprintf("2025-01-%02d", i+1))
		mustExec(t, db, ctx, `UPDATE articles SET published_revision_id=$1,pinned=$2 WHERE id=$3`, revision, i < 2, id)
	}
	if CheckReady(ctx, db) == nil {
		t.Fatal("schema 10 accepted by new binary")
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err := CheckReady(ctx, db); err != nil {
		t.Fatal(err)
	}
	var ordered string
	if err := db.QueryRowContext(ctx, `SELECT string_agg(id::text,',' ORDER BY pin_position) FROM articles WHERE pinned`).Scan(&ordered); err != nil || ordered != ids[1]+","+ids[0] {
		t.Fatalf("backfill order %q: %v", ordered, err)
	}
	var unchanged int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM articles a JOIN revisions r ON r.id=a.published_revision_id WHERE a.version=1 AND a.published_at='2025-01-01' AND a.updated_at='2025-02-01' AND r.markdown='Unchanged body'`).Scan(&unchanged); err != nil || unchanged != 3 {
		t.Fatal("migration altered existing content/dates/versions", err)
	}
	// The previous binary can still pin and unpublish without writing positions.
	mustExec(t, db, ctx, `UPDATE articles SET pinned=true WHERE id=$1`, ids[2])
	var last string
	if err := db.QueryRowContext(ctx, `SELECT id FROM articles WHERE pinned ORDER BY pin_position DESC LIMIT 1`).Scan(&last); err != nil || last != ids[2] {
		t.Fatal("legacy pin did not append", err)
	}
	mustExec(t, db, ctx, `UPDATE articles SET published_revision_id=NULL WHERE id=$1`, ids[1])
	var cleared bool
	if err := db.QueryRowContext(ctx, `SELECT NOT pinned AND pin_position IS NULL FROM articles WHERE id=$1`, ids[1]).Scan(&cleared); err != nil || !cleared {
		t.Fatal("legacy unpublish retained position", err)
	}
	mustReject(t, db, ctx, "23514", `UPDATE articles SET pin_position=-1 WHERE id=$1`, ids[0])
	mustReject(t, db, ctx, "23505", `UPDATE articles SET pin_position=(SELECT pin_position FROM articles WHERE id=$1) WHERE id=$2`, ids[0], ids[2])
	if pending, err := provider.Up(ctx); err != nil || len(pending) != 0 {
		t.Fatal("repeat migration changed state", err)
	}
	// Rehearse the new runtime sequence grant with an unprivileged role. Role,
	// grants and mutations are transactional and disappear on rollback.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var schema string
	if err := tx.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	role := pgx.Identifier{fmt.Sprintf("isaber_pin_runtime_%d", time.Now().UnixNano())}.Sanitize()
	for _, statement := range []string{
		`CREATE ROLE ` + role + ` NOLOGIN`,
		`GRANT USAGE ON SCHEMA ` + pgx.Identifier{schema}.Sanitize() + ` TO ` + role,
		`GRANT SELECT,UPDATE ON articles TO ` + role,
		`GRANT USAGE ON SEQUENCE article_pin_position_seq TO ` + role,
		`SET LOCAL ROLE ` + role,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE articles SET pinned=false WHERE id=$1`, ids[0]); err != nil {
		t.Fatal(err)
	}
	var position int64
	if err := tx.QueryRowContext(ctx, `UPDATE articles SET pinned=true WHERE id=$1 RETURNING pin_position`, ids[0]).Scan(&position); err != nil || position < 1 {
		t.Fatal("restricted runtime cannot append a pin", err)
	}
}
