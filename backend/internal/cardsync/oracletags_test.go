package cardsync_test

import (
	"context"
	"os"
	"sort"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"manafold-backend/internal/cardsearch"
	"manafold-backend/internal/cardsync"
	db "manafold-backend/internal/db/generated"
)

const (
	goblinOracle = "11111111-1111-1111-1111-111111111111" // mana-rock (identity R)
	ratOracle    = "22222222-2222-2222-2222-222222222222" // mana-dork (identity B)
	septetOracle = "33333333-3333-3333-3333-333333333333" // cycle-b
	walkerOracle = "44444444-4444-4444-4444-444444444444" // extra-turn
	manaRockTag  = "a0000000-0000-0000-0000-000000000002"
	staleTag     = "a0000000-0000-0000-0000-0000000000ff"
)

func tagFixtureOptions() cardsync.Options {
	return cardsync.Options{
		OracleCardsPath:  "testdata/oracle_cards.jsonl",
		DefaultCardsPath: "testdata/default_cards.jsonl",
		OracleTagsPath:   "testdata/oracle_tags.jsonl",
	}
}

// TestRun_IngestsOracleTags runs the oracle-tags pass over the checked-in tag
// fixture and checks the mirrored snapshot: direct taggings with their weight
// and annotation, orphan and malformed taggings skipped and counted, non-oracle
// tags skipped, the closure walking parent links (and terminating on a cycle),
// the query surface resolving descendants, and the sync_runs audit row.
//
// @spec CARD-010, CARD-060, CARD-061, CARD-062, CARD-065
func TestRun_IngestsOracleTags(t *testing.T) {
	ctx, pool := tagTestPool(t)
	q := db.New(pool)

	res, err := cardsync.Run(ctx, pool, tagFixtureOptions())
	if err != nil {
		t.Fatalf("cardsync.Run: %v", err)
	}
	if res.TagsUpserted != 6 {
		t.Errorf("TagsUpserted = %d, want 6 (the illustration tag is skipped)", res.TagsUpserted)
	}
	if res.TaggingsUpserted != 4 {
		t.Errorf("TaggingsUpserted = %d, want 4", res.TaggingsUpserted)
	}
	if res.TaggingsSkipped != 2 {
		t.Errorf("TaggingsSkipped = %d, want 2 (one orphan oracle_id, one unparseable)", res.TaggingsSkipped)
	}

	var artTags int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM oracle_tags WHERE slug = 'art-only'`).Scan(&artTags); err != nil {
		t.Fatalf("count art tag: %v", err)
	}
	if artTags != 0 {
		t.Errorf("non-oracle tag mirrored %d time(s), want 0 (CARD-061)", artTags)
	}

	var parents []pgtype.UUID
	var aliases []string
	if err := pool.QueryRow(ctx,
		`SELECT parent_ids, aliases FROM oracle_tags WHERE slug = 'mana-rock'`).Scan(&parents, &aliases); err != nil {
		t.Fatalf("read mana-rock tag: %v", err)
	}
	if len(parents) != 1 || parents[0] != mustUUID(t, "a0000000-0000-0000-0000-000000000001") {
		t.Errorf("mana-rock parent_ids = %v, want [ramp]", parents)
	}

	rat := cardIDByOracle(t, ctx, q, ratOracle)
	tags, err := q.ListOracleTagsForCard(ctx, rat)
	if err != nil {
		t.Fatalf("ListOracleTagsForCard: %v", err)
	}
	if len(tags) != 1 || tags[0].Slug != "mana-dork" || tags[0].Weight != "very_strong" {
		t.Errorf("rat tags = %+v, want [mana-dork very_strong]", tags)
	}
	var annotation string
	if err := pool.QueryRow(ctx,
		`SELECT annotation FROM card_oracle_tags WHERE card_id = $1`, rat).Scan(&annotation); err != nil {
		t.Fatalf("read annotation: %v", err)
	}
	if annotation != "taps for B" {
		t.Errorf("annotation = %q, want \"taps for B\"", annotation)
	}

	// "ramp" has no direct taggings; its cards come through the closure.
	if got := cardsByTag(t, ctx, q, "ramp", nil); !equalStrings(got, []string{"Manafold Test Goblin Boss", "Manafold Test Rat Swarm"}) {
		t.Errorf("cards by ramp = %v, want goblin + rat via descendants (CARD-062)", got)
	}
	if got := cardsByTag(t, ctx, q, "RAMP", []string{"R"}); !equalStrings(got, []string{"Manafold Test Goblin Boss"}) {
		t.Errorf("cards by ramp within {R} = %v, want just the goblin", got)
	}
	// The cycle closes without looping forever, and each half reaches the other.
	if got := cardsByTag(t, ctx, q, "cycle-a", nil); !equalStrings(got, []string{"Manafold Test Septet"}) {
		t.Errorf("cards by cycle-a = %v, want the septet via cycle-b", got)
	}

	goblin := cardIDByOracle(t, ctx, q, goblinOracle)
	septet := cardIDByOracle(t, ctx, q, septetOracle)
	walker := cardIDByOracle(t, ctx, q, walkerOracle)
	ids, err := q.FilterCardIDsByOracleTag(ctx, db.FilterCardIDsByOracleTagParams{
		Slug: "ramp", CardIds: []pgtype.UUID{goblin, septet, walker},
	})
	if err != nil {
		t.Fatalf("FilterCardIDsByOracleTag: %v", err)
	}
	if len(ids) != 1 || ids[0] != goblin {
		t.Errorf("ramp among {goblin, septet, walker} = %v, want [goblin]", ids)
	}

	run, err := q.LatestSyncRun(ctx, "oracle_tags")
	if err != nil {
		t.Fatalf("latest oracle_tags sync run: %v", err)
	}
	if run.Status != "succeeded" || run.RowsUpserted != 4 {
		t.Errorf("oracle_tags sync run = {status %q, rows %d}, want {succeeded, 4}", run.Status, run.RowsUpserted)
	}
}

// TestRun_OracleTagsReplaceSnapshotAndKeepDisabled re-runs the tag pass over a
// mirror that holds a tag and tagging the export no longer lists and a tag a
// maintainer has disabled: the stale rows leave, the counts are unchanged
// (idempotent), the disabled flag survives the sync, and every tag read —
// including otag: search — ignores the disabled tag.
//
// @spec CARD-060, CARD-063, CARD-064
func TestRun_OracleTagsReplaceSnapshotAndKeepDisabled(t *testing.T) {
	ctx, pool := tagTestPool(t)
	q := db.New(pool)

	if _, err := cardsync.Run(ctx, pool, tagFixtureOptions()); err != nil {
		t.Fatalf("first cardsync.Run: %v", err)
	}
	before := tagTableCounts(t, ctx, pool)

	goblin := cardIDByOracle(t, ctx, q, goblinOracle)
	if _, err := pool.Exec(ctx,
		`INSERT INTO oracle_tags (id, slug, label) VALUES ($1, 'stale-tag', 'stale tag')`,
		mustUUID(t, staleTag)); err != nil {
		t.Fatalf("insert stale tag: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO card_oracle_tags (card_id, tag_id, weight) VALUES ($1, $2, 'median')`,
		goblin, mustUUID(t, staleTag)); err != nil {
		t.Fatalf("insert stale tagging: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM card_oracle_tags WHERE card_id = $1 AND tag_id = $2`,
		cardIDByOracle(t, ctx, q, walkerOracle), mustUUID(t, "a0000000-0000-0000-0000-000000000004")); err != nil {
		t.Fatalf("drop a tagging: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE oracle_tags SET disabled = true WHERE id = $1`, mustUUID(t, manaRockTag)); err != nil {
		t.Fatalf("disable mana-rock: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `UPDATE oracle_tags SET disabled = false WHERE id = $1`, mustUUID(t, manaRockTag))
	})

	if _, err := cardsync.Run(ctx, pool, tagFixtureOptions()); err != nil {
		t.Fatalf("second cardsync.Run: %v", err)
	}
	if after := tagTableCounts(t, ctx, pool); after != before {
		t.Errorf("table counts after re-run = %v, want %v (idempotent; stale rows gone, dropped tagging restored)", after, before)
	}

	var stale int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM oracle_tags WHERE id = $1`, mustUUID(t, staleTag)).Scan(&stale); err != nil {
		t.Fatalf("count stale tag: %v", err)
	}
	if stale != 0 {
		t.Errorf("stale tag survived the sync")
	}

	var disabled bool
	if err := pool.QueryRow(ctx, `SELECT disabled FROM oracle_tags WHERE id = $1`, mustUUID(t, manaRockTag)).Scan(&disabled); err != nil {
		t.Fatalf("read disabled: %v", err)
	}
	if !disabled {
		t.Fatalf("the sync overwrote mana-rock's disabled flag (CARD-063)")
	}

	if tags, err := q.ListOracleTagsForCard(ctx, goblin); err != nil || len(tags) != 0 {
		t.Errorf("goblin tags with mana-rock disabled = %+v (err %v), want none", tags, err)
	}
	if got := cardsByTag(t, ctx, q, "ramp", nil); !equalStrings(got, []string{"Manafold Test Rat Swarm"}) {
		t.Errorf("cards by ramp with mana-rock disabled = %v, want just the rat", got)
	}
	if got := cardsByTag(t, ctx, q, "mana-rock", nil); len(got) != 0 {
		t.Errorf("cards by disabled mana-rock = %v, want none", got)
	}
	if ids, err := q.FilterCardIDsByOracleTag(ctx, db.FilterCardIDsByOracleTagParams{
		Slug: "ramp", CardIds: []pgtype.UUID{goblin},
	}); err != nil || len(ids) != 0 {
		t.Errorf("goblin counted as ramp with mana-rock disabled: %v (err %v)", ids, err)
	}

	// otag: search runs the same closure lookup over the mirror.
	if got := searchNames(t, ctx, pool, "otag:ramp"); !equalStrings(got, []string{"Manafold Test Rat Swarm"}) {
		t.Errorf("otag:ramp = %v, want just the rat", got)
	}
	if got := searchNames(t, ctx, pool, "oracletag:extra-turn"); !equalStrings(got, []string{"Manafold Test Walker"}) {
		t.Errorf("oracletag:extra-turn = %v, want the walker", got)
	}
}

// TestRun_FixtureCardsWithoutTagFileSkipsTagPass: a run whose card passes both
// read local files and that names no tag file must not reach for the network
// (no BaseURL is reachable here) and records no oracle_tags run.
//
// @spec CARD-010
func TestRun_FixtureCardsWithoutTagFileSkipsTagPass(t *testing.T) {
	ctx, pool := tagTestPool(t)

	res, err := cardsync.Run(ctx, pool, cardsync.Options{
		BaseURL:          "http://127.0.0.1:1", // unreachable: any fetch fails the run
		OracleCardsPath:  "testdata/oracle_cards.jsonl",
		DefaultCardsPath: "testdata/default_cards.jsonl",
	})
	if err != nil {
		t.Fatalf("cardsync.Run without a tag file: %v", err)
	}
	if res.TagsUpserted != 0 || res.TaggingsUpserted != 0 {
		t.Errorf("tag pass ran without a tag file: %+v", res)
	}
}

// TestRun_OracleTagsBrokenExportFailsAndKeepsSnapshot: an export that decodes
// to no oracle tags is a broken file, not an instruction to wipe the mirror —
// the run fails, the sync_runs row records it, and the prior snapshot stays.
//
// @spec CARD-007, CARD-060
func TestRun_OracleTagsBrokenExportFailsAndKeepsSnapshot(t *testing.T) {
	ctx, pool := tagTestPool(t)
	q := db.New(pool)

	if _, err := cardsync.Run(ctx, pool, tagFixtureOptions()); err != nil {
		t.Fatalf("first cardsync.Run: %v", err)
	}
	before := tagTableCounts(t, ctx, pool)

	empty := t.TempDir() + "/oracle_tags.jsonl"
	if err := os.WriteFile(empty, []byte(`{"object":"tag","id":"a0000000-0000-0000-0000-000000000007","type":"illustration","slug":"art-only","label":"art only","parent_ids":[],"aliases":[],"taggings":[]}`), 0o600); err != nil {
		t.Fatalf("write broken export: %v", err)
	}
	opts := tagFixtureOptions()
	opts.OracleTagsPath = empty
	if _, err := cardsync.Run(ctx, pool, opts); err == nil {
		t.Fatalf("cardsync.Run over an export with no oracle tags succeeded, want an error")
	}
	if after := tagTableCounts(t, ctx, pool); after != before {
		t.Errorf("tag tables after a failed run = %v, want the prior snapshot %v", after, before)
	}
	run, err := q.LatestSyncRun(ctx, "oracle_tags")
	if err != nil {
		t.Fatalf("latest oracle_tags sync run: %v", err)
	}
	if run.Status != "failed" || !run.Error.Valid || !run.FinishedAt.Valid {
		t.Errorf("oracle_tags sync run = {status %q, error %v, finished %v}, want failed with error text and finished_at",
			run.Status, run.Error.Valid, run.FinishedAt.Valid)
	}
}

func tagTestPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	_ = godotenv.Load("../../.env")
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping integration test (see backend/.env.example)")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return ctx, pool
}

func cardIDByOracle(t *testing.T, ctx context.Context, q *db.Queries, oracleID string) pgtype.UUID {
	t.Helper()
	card, err := q.GetCardByScryfallOracleID(ctx, mustUUID(t, oracleID))
	if err != nil {
		t.Fatalf("card for oracle_id %s: %v", oracleID, err)
	}
	return card.ID
}

func cardsByTag(t *testing.T, ctx context.Context, q *db.Queries, slug string, identity []string) []string {
	t.Helper()
	if identity == nil {
		identity = []string{}
	}
	cards, err := q.ListCardsByOracleTag(ctx, db.ListCardsByOracleTagParams{Slug: slug, Identity: identity, Lim: 100})
	if err != nil {
		t.Fatalf("ListCardsByOracleTag(%q): %v", slug, err)
	}
	var names []string
	for _, c := range cards {
		names = append(names, c.Name)
	}
	return names
}

func searchNames(t *testing.T, ctx context.Context, pool *pgxpool.Pool, raw string) []string {
	t.Helper()
	query, err := cardsearch.Parse(raw)
	if err != nil {
		t.Fatalf("Parse(%q): %v", raw, err)
	}
	where, args, _ := query.WhereSQL(1)
	rows, err := pool.Query(ctx, `SELECT c.name FROM cards c WHERE `+where, args...)
	if err != nil {
		t.Fatalf("search %q: %v", raw, err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		names = append(names, n)
	}
	return names
}

type tagCounts struct{ tags, taggings, closure int }

func tagTableCounts(t *testing.T, ctx context.Context, pool *pgxpool.Pool) tagCounts {
	t.Helper()
	var c tagCounts
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM oracle_tags),
		(SELECT count(*) FROM card_oracle_tags),
		(SELECT count(*) FROM oracle_tag_closure)`).Scan(&c.tags, &c.taggings, &c.closure); err != nil {
		t.Fatalf("count tag tables: %v", err)
	}
	return c
}

func equalStrings(got, want []string) bool {
	g := append([]string(nil), got...)
	w := append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	if len(g) != len(w) {
		return false
	}
	for i := range g {
		if g[i] != w[i] {
			return false
		}
	}
	return true
}
