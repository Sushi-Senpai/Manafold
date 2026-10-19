package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSeedOptions_TagFile: CARDSYNC_TAGS_PATH names the tag file; without it a
// seed run picks up the oracle_tags.json beside the seed file, and a seed with
// no such sibling runs with no tag file (the tag pass is then skipped).
//
// @spec CARD-066
func TestSeedOptions_TagFile(t *testing.T) {
	dir := t.TempDir()
	seed := filepath.Join(dir, "cards.json")
	sibling := filepath.Join(dir, "oracle_tags.json")

	t.Setenv("CARDSYNC_ORACLE_PATH", "")
	t.Setenv("CARDSYNC_DEFAULT_PATH", "")
	t.Setenv("CARDSYNC_TAGS_PATH", "")
	t.Setenv("CARDSYNC_SEED_PATH", seed)

	if got := seedOptions(); got.OracleTagsPath != "" {
		t.Errorf("no sibling tag file: OracleTagsPath = %q, want empty", got.OracleTagsPath)
	}

	if err := os.WriteFile(sibling, []byte("[]"), 0o600); err != nil {
		t.Fatalf("write sibling: %v", err)
	}
	got := seedOptions()
	if got.OracleTagsPath != sibling || got.OracleCardsPath != seed || got.DefaultCardsPath != seed {
		t.Errorf("seed with sibling tags = %+v, want both card passes on %q and tags on %q", got, seed, sibling)
	}

	t.Setenv("CARDSYNC_TAGS_PATH", "/explicit/tags.jsonl")
	if got := seedOptions(); got.OracleTagsPath != "/explicit/tags.jsonl" {
		t.Errorf("explicit CARDSYNC_TAGS_PATH: OracleTagsPath = %q", got.OracleTagsPath)
	}

	t.Setenv("CARDSYNC_SEED_PATH", "")
	if got := seedOptions(); got.OracleTagsPath != "/explicit/tags.jsonl" || got.OracleCardsPath != "" {
		t.Errorf("tags path alone = %+v, want only OracleTagsPath set", got)
	}
}
