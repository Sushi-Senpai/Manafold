# PATCH /api/decks/{id}/cards/{cardId} — quantity bounds (nm-add-1 fix)

Exercised against the real Go API (`DEV_AUTH=true`, chi router) backed by
Postgres seeded from `backend/seed/cards.json`. The card is the seeded real
"Sol Ring" print; a fresh deck was created and the card added to the mainboard.

## Oversized quantity — 2^40 (1099511627776), previously wrapped by int32() to 0
$ curl -i -X PATCH .../decks/{id}/cards/{cardId} -d '{"board":"main","quantity":1099511627776}'
HTTP/1.1 400 Bad Request
Content-Type: application/json
{"error":"quantity is too large"}

## Negative quantity (pre-existing guard, still works)
$ curl -i -X PATCH .../decks/{id}/cards/{cardId} -d '{"board":"main","quantity":-3}'
HTTP/1.1 400 Bad Request
{"error":"quantity must be zero or greater"}

## Sane quantity 4 (control) — accepted, entry updated in place
$ curl -i -X PATCH .../decks/{id}/cards/{cardId} -d '{"board":"main","quantity":4}'
HTTP/1.1 204 No Content

GET /api/decks/{id} afterwards shows the mainboard Sol Ring entry with "quantity": 4.

## Regression check
With the new `q > maxDeckCardQuantity` guard removed, the same 2^40 request
returns `HTTP 500 {"error":"failed to update quantity"}` (int32(2^40) == 0 hits
the deck_cards `quantity > 0` CHECK constraint). The guard turns it into a clean
400. `go test ./internal/api/ -run TestPatchCard_QuantityAndBoardMove` fails
before the guard, passes after.
