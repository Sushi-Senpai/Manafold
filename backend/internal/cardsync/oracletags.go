package cardsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "manafold-backend/internal/db/generated"
)

// scryfallTag is one object of the oracle_tags bulk export: a Tagger tag with
// its direct taggings only. Parent tags are rolled up at read time by the
// oracle_tag_reach walk, not here.
type scryfallTag struct {
	ID          string   `json:"id"`
	Type        string   `json:"type"`
	Slug        string   `json:"slug"`
	Label       string   `json:"label"`
	Description *string  `json:"description"`
	ParentIDs   []string `json:"parent_ids"`
	Aliases     []string `json:"aliases"`
	Taggings    []struct {
		OracleID   string  `json:"oracle_id"`
		Weight     string  `json:"weight"`
		Annotation *string `json:"annotation"`
	} `json:"taggings"`
}

// stagedTagging is the cleaned tagging shape carried in the staging table's
// jsonb column and exploded by tagMergeTaggingsSQL. oracle_id is always a
// parseable UUID, so the SQL cast cannot fail the merge.
type stagedTagging struct {
	OracleID   string  `json:"oracle_id"`
	Weight     string  `json:"weight"`
	Annotation *string `json:"annotation,omitempty"`
}

// syncOracleTags runs the tag pass over the local file at path, or — with no
// path — over the manifest's oracle_tags export. A failure to open or fetch the
// export records a failed oracle_tags sync_runs row like an ingest failure
// does, so the caller can report it without failing the run (CARD-067).
//
// @spec CARD-010, CARD-067
func syncOracleTags(ctx context.Context, pool *pgxpool.Pool, q *db.Queries, f *fetcher, man *bulkManifest, path string) (tags, taggings, skipped int, err error) {
	var rc io.ReadCloser
	var updatedAt time.Time
	if path != "" {
		rc, err = os.Open(path)
	} else {
		var cleanup func()
		rc, updatedAt, cleanup, err = f.fetchBulk(ctx, *man, "oracle_tags")
		if err == nil {
			defer cleanup()
		}
	}
	if err != nil {
		if run, cerr := q.CreateSyncRun(ctx, db.CreateSyncRunParams{
			BulkType: "oracle_tags", ScryfallUpdatedAt: tstz(updatedAt),
		}); cerr == nil {
			_ = q.FailSyncRun(ctx, db.FailSyncRunParams{ID: run.ID, Error: text(err.Error())})
		}
		return 0, 0, 0, fmt.Errorf("oracle_tags fetch: %w", err)
	}
	defer rc.Close()
	return ingestOracleTags(ctx, pool, q, rc, updatedAt)
}

// ingestOracleTags ingests the oracle_tags export, recording a sync_runs row
// (CARD-010, CARD-007). The snapshot replaces the mirrored one in a single
// transaction (CARD-060); a tagging whose oracle_id is unparseable or
// matches no cards row is skipped and counted (CARD-061).
//
// @spec CARD-010, CARD-060, CARD-061
func ingestOracleTags(ctx context.Context, pool *pgxpool.Pool, q *db.Queries, r io.Reader, updatedAt time.Time) (tags, taggings, skipped int, err error) {
	run, err := q.CreateSyncRun(ctx, db.CreateSyncRunParams{
		BulkType:          "oracle_tags",
		ScryfallUpdatedAt: tstz(updatedAt),
	})
	if err != nil {
		return 0, 0, 0, err
	}

	tags, taggings, skipped, ingestErr := copyReplaceOracleTags(ctx, pool, r)
	if ingestErr != nil {
		_ = q.FailSyncRun(ctx, db.FailSyncRunParams{
			ID: run.ID, Error: text(ingestErr.Error()), RowsUpserted: int32(taggings),
		})
		return 0, 0, 0, fmt.Errorf("oracle_tags ingest: %w", ingestErr)
	}
	if err := q.FinishSyncRun(ctx, db.FinishSyncRunParams{ID: run.ID, RowsUpserted: int32(taggings)}); err != nil {
		return 0, 0, 0, err
	}
	return tags, taggings, skipped, nil
}

// tagImportDDL creates the session-scoped staging table every decoded tag is
// COPYd into, its cleaned taggings riding along as one jsonb value so the
// export is streamed once.
const tagImportDDL = `CREATE TEMP TABLE oracle_tags_import (
	id          uuid NOT NULL,
	slug        text NOT NULL,
	label       text NOT NULL,
	description text,
	parent_ids  uuid[] NOT NULL,
	aliases     text[] NOT NULL,
	taggings    jsonb NOT NULL
) ON COMMIT DROP`

// tagImportColumns is the COPY column order; it must match the row
// copyReplaceOracleTags builds.
var tagImportColumns = []string{"id", "slug", "label", "description", "parent_ids", "aliases", "taggings"}

// The merge statements, run in order inside one transaction. Deleting absent
// tags first lets the cascade clear their taggings; the upsert
// never touches the Manafold-owned disabled flag (CARD-063).
const (
	tagDeleteMissingSQL = `DELETE FROM oracle_tags t
WHERE NOT EXISTS (SELECT 1 FROM oracle_tags_import i WHERE i.id = t.id)`

	tagUpsertSQL = `
INSERT INTO oracle_tags (id, slug, label, description, parent_ids, aliases)
SELECT DISTINCT ON (i.id) i.id, i.slug, i.label, i.description, i.parent_ids, i.aliases
FROM oracle_tags_import i
ORDER BY i.id
ON CONFLICT (id) DO UPDATE SET
	slug        = EXCLUDED.slug,
	label       = EXCLUDED.label,
	description = EXCLUDED.description,
	parent_ids  = EXCLUDED.parent_ids,
	aliases     = EXCLUDED.aliases`

	tagClearTaggingsSQL = `DELETE FROM card_oracle_tags`

	// The JOIN on cards resolves card_id and drops taggings on objects the
	// Oracle export omits (CARD-061).
	tagMergeTaggingsSQL = `
INSERT INTO card_oracle_tags (card_id, tag_id, weight, annotation)
SELECT DISTINCT ON (c.id, i.id) c.id, i.id, t.weight, t.annotation
FROM oracle_tags_import i
CROSS JOIN LATERAL jsonb_to_recordset(i.taggings) AS t(oracle_id uuid, weight text, annotation text)
JOIN cards c ON c.scryfall_oracle_id = t.oracle_id
ORDER BY c.id, i.id`
)

// copyReplaceOracleTags streams every oracle tag from r into a TEMP staging
// table with COPY, then replaces the mirrored tags and taggings with set-based
// statements in the same transaction (CARD-060). It
// returns the tags upserted, the taggings written, and the taggings skipped:
// those with an unparseable oracle_id or no weight (dropped before staging)
// plus staged taggings whose oracle_id matched no cards row (CARD-061).
func copyReplaceOracleTags(ctx context.Context, pool *pgxpool.Pool, r io.Reader) (tags, taggings, skipped int, err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, tagImportDDL); err != nil {
		return 0, 0, 0, fmt.Errorf("create tag staging table: %w", err)
	}

	stream := newJSONObjectStream(r)
	var malformed, stagedTaggings int
	src := pgx.CopyFromFunc(func() ([]any, error) {
		for {
			raw, nerr := stream.next()
			if errors.Is(nerr, io.EOF) {
				return nil, nil
			}
			if nerr != nil {
				return nil, nerr
			}
			var tag scryfallTag
			if uerr := json.Unmarshal(raw, &tag); uerr != nil {
				return nil, uerr
			}
			id, ok := parseUUID(tag.ID)
			if !ok || tag.Type != "oracle" {
				continue
			}
			parents := make([]pgtype.UUID, 0, len(tag.ParentIDs))
			for _, p := range tag.ParentIDs {
				if pu, ok := parseUUID(p); ok {
					parents = append(parents, pu)
				}
			}
			clean := make([]stagedTagging, 0, len(tag.Taggings))
			for _, tg := range tag.Taggings {
				if _, ok := parseUUID(tg.OracleID); !ok || tg.Weight == "" {
					malformed++
					continue
				}
				clean = append(clean, stagedTagging{OracleID: tg.OracleID, Weight: tg.Weight, Annotation: tg.Annotation})
			}
			stagedTaggings += len(clean)
			taggingsJSON, merr := json.Marshal(clean)
			if merr != nil {
				return nil, merr
			}
			label := tag.Label
			if label == "" {
				label = tag.Slug
			}
			return []any{id, tag.Slug, label, textPtr(tag.Description), parents, orEmpty(tag.Aliases), json.RawMessage(taggingsJSON)}, nil
		}
	})

	staged, err := tx.CopyFrom(ctx, pgx.Identifier{"oracle_tags_import"}, tagImportColumns, src)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("stage tags: %w", err)
	}
	// An export that decodes to no tags is a broken file, not "Scryfall deleted
	// every tag": refuse it rather than wipe the mirror.
	if staged == 0 {
		return 0, 0, 0, errors.New("oracle_tags export contained no oracle tags")
	}

	if _, err := tx.Exec(ctx, tagDeleteMissingSQL); err != nil {
		return 0, 0, 0, fmt.Errorf("delete absent tags: %w", err)
	}
	upserted, err := tx.Exec(ctx, tagUpsertSQL)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("upsert tags: %w", err)
	}
	tags = int(upserted.RowsAffected())
	if _, err := tx.Exec(ctx, tagClearTaggingsSQL); err != nil {
		return 0, 0, 0, fmt.Errorf("clear taggings: %w", err)
	}
	merged, err := tx.Exec(ctx, tagMergeTaggingsSQL)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("merge taggings: %w", err)
	}
	taggings = int(merged.RowsAffected())

	if err := tx.Commit(ctx); err != nil {
		return 0, 0, 0, err
	}

	// Every staged tagging that did not merge is an orphan (its oracle_id is
	// not in cards); add the ones dropped before staging. Scryfall's export
	// carries no duplicate (card, tag) taggings, so the difference is the
	// orphan count.
	skipped = malformed + stagedTaggings - taggings
	return tags, taggings, skipped, nil
}
