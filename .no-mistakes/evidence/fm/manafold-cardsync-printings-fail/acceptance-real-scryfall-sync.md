# Acceptance: `go run ./cmd/cardsync` against the real Scryfall bulk API

Branch: fm/manafold-cardsync-printings-fail @ bcf2924
DB: local Postgres (embedded, port 56432), migrations at version 5.

## Reported failure (before this change)
Default Cards pass died partway with `stream error: ... PROTOCOL_ERROR; received
from peer`, non-zero exit, leaving only ~6k card_prints rows (~87% of cards with
no image_uris/prices).

## Full sync run (this change)

    $ DATABASE_URL=postgres://manafold:manafold@localhost:56432/manafold?sslmode=disable \
        /usr/bin/time -v go run ./cmd/cardsync
    2026/09/10 15:37:47 card sync complete: 38681 cards, 117748 prints upserted, 81 prints skipped
    Elapsed (wall clock) time: 2:40.22
    Exit status: 0

Both passes completed. No PROTOCOL_ERROR / stream error. Process exited 0.

## Mirror state after the run (Postgres)

    cards total                                  38799
    card_prints total                            117753
    card_prints WITH image_uris                  113590   (~96%; remainder are genuinely imageless Scryfall printings)
    card_prints WITH prices                      117753   (100%)
    distinct cards having >=1 print              38683    (99.7% of 38799 cards)

    sync_runs (most recent):
      default_cards   succeeded   rows=117748
      oracle_cards    succeeded   rows=38681

card_prints is populated for essentially every card (38,683 / 38,799), versus
the ~6k rows of the reported failure.

## Spool cleanup
No `cardsync-*.jsonl.gz` temp files left in /tmp after the run (CARD-013: spool
removed on the success path).
