package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"manafold-backend/internal/ai"
	db "manafold-backend/internal/db/generated"
	"manafold-backend/internal/deckroles"
	"manafold-backend/internal/deckwizard"
)

// Oracle texts the default role classifier recognises.
const (
	rampText  = "{T}: Add {C}{C}."
	drawText  = "Draw three cards."
	spotText  = "Destroy target creature."
	wipeText  = "Destroy all creatures."
	plainText = "Flying"
)

func (fx *wizFixture) roleCard(name, typeLine, text string, identity ...string) wizCard {
	return fx.card(wizCardSpec{name: name, typeLine: typeLine, text: text, identity: identity, usd: f64(1)})
}

func roleRow(t *testing.T, rows []deckwizard.RoleRow, role deckroles.Role) deckwizard.RoleRow {
	t.Helper()
	for _, r := range rows {
		if r.Role == role {
			return r
		}
	}
	t.Fatalf("no %s row in %+v", role, rows)
	return deckwizard.RoleRow{}
}

// @spec WIZ-070
func TestWizardPlan_RolesAgainstTheArchetypeTemplate(t *testing.T) {
	fx := newWizFixture(t)
	deckID, _ := fx.deck([]string{"W"}, 3, "midrange_value")

	rock := fx.roleCard("Role Rock", "Artifact", rampText, "W")
	both := fx.roleCard("Role Both", "Sorcery", "Draw two cards. Destroy target creature.", "W")
	wipe := fx.roleCard("Role Wipe", "Sorcery", wipeText, "W")
	fx.add(deckID, rock, "main", 3)
	fx.add(deckID, both, "main", 1) // a card filling two roles counts toward both
	fx.add(deckID, wipe, "maybe", 1)
	fx.add(deckID, wipe, "sideboard", 1) // only main counts

	p := fx.get(deckID)
	if len(p.Roles) != 4 {
		t.Fatalf("roles = %+v, want 4 rows", p.Roles)
	}
	ramp := roleRow(t, p.Roles, deckroles.Ramp)
	if ramp.Count != 3 || ramp.Min != 10 || ramp.Max != 12 || ramp.Status != "under" {
		t.Errorf("ramp row = %+v", ramp)
	}
	if d := roleRow(t, p.Roles, deckroles.CardDraw); d.Count != 1 {
		t.Errorf("draw row = %+v", d)
	}
	if r := roleRow(t, p.Roles, deckroles.TargetedRemoval); r.Count != 1 {
		t.Errorf("removal row = %+v", r)
	}
	if w := roleRow(t, p.Roles, deckroles.BoardWipe); w.Count != 0 {
		t.Errorf("wipe row = %+v", w)
	}

	// The same counts the deck-vs-target panel reports.
	rec := serve(t, fx.a, fx.owner, http.MethodGet, "/decks/"+deckID+"/template-fit", nil)
	fit := decode[templateFitJSON](t, rec)
	for _, row := range fit.Rows {
		for _, r := range p.Roles {
			if row.Key == string(r.Role) && row.Count != r.Count {
				t.Errorf("%s: wizard %d, template-fit %d", r.Role, r.Count, row.Count)
			}
		}
	}

	none, _ := fx.deck([]string{"W"}, 3, "")
	if p := fx.get(none); p.Roles == nil || len(p.Roles) != 0 {
		t.Errorf("no archetype: roles = %#v, want an empty list", p.Roles)
	}
}

// @spec WIZ-071
func TestWizardPropose_RolesNeedAKnownRole(t *testing.T) {
	fx := newWizFixture(t)
	fx.assistant(shouldNotBeCalled(t))
	deckID, _ := fx.deck([]string{"W"}, 3, "midrange_value")
	for label, body := range map[string]map[string]any{
		"missing role": {"step": "roles"},
		"unknown role": {"step": "roles", "role": "lifegain"},
		"wrong case":   {"step": "roles", "role": "Ramp"},
	} {
		if rec := fx.proposeRaw(deckID, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", label, rec.Code, rec.Body.String())
		}
	}
	if n := fx.usage("wizard"); n != 0 {
		t.Errorf("metered %d calls for rejected bodies", n)
	}
}

// @spec WIZ-071
func TestWizardPropose_RolesKeepTheAIGates(t *testing.T) {
	fx := newWizFixture(t)
	deckID, _ := fx.deck([]string{"W"}, 3, "midrange_value")
	fx.a.AI = fakeAssistant{enabled: false}
	if rec := fx.proposeRaw(deckID, map[string]any{"step": "roles", "role": "ramp"}); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("assistant disabled = %d, want 503", rec.Code)
	}
	noBracket, _ := fx.deck([]string{"W"}, 0, "midrange_value")
	fx.assistant(shouldNotBeCalled(t))
	if rec := fx.proposeRaw(noBracket, map[string]any{"step": "roles", "role": "ramp"}); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("no bracket = %d, want 422", rec.Code)
	}
}

// @spec WIZ-072
func TestWizardPropose_RolesEmptyAnswersSkipTheModel(t *testing.T) {
	fx := newWizFixture(t)
	fx.assistant(shouldNotBeCalled(t))
	expect := func(label string, deckID, role, reason string) {
		t.Helper()
		rec := fx.proposeRaw(deckID, map[string]any{"step": "roles", "role": role})
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", label, rec.Code, rec.Body.String())
		}
		got := decode[wizardProposeResponse](t, rec)
		if got.EmptyReason == nil || *got.EmptyReason != reason || len(got.Proposals) != 0 || got.Role != role {
			t.Errorf("%s: role=%q empty_reason=%v proposals=%d, want %q and none", label, got.Role, got.EmptyReason, len(got.Proposals), reason)
		}
	}

	noArchetype, _ := fx.deck([]string{"W"}, 3, "")
	fx.roleCard("Empty Rock", "Artifact", rampText, "W")
	expect("no archetype", noArchetype, "ramp", "no_archetype")

	// Midrange wants at most 12 ramp cards.
	atMax, _ := fx.deck([]string{"W"}, 3, "midrange_value")
	rock := fx.roleCard("Max Rock", "Artifact", rampText, "W")
	fx.add(atMax, rock, "main", 12)
	expect("ramp at its maximum", atMax, "ramp", "role_filled")

	full, _ := fx.deck([]string{"W"}, 3, "midrange_value")
	filler := fx.roleCard("Full Filler", "Creature — Test", plainText, "W")
	fx.add(full, filler, "main", 100)
	expect("deck full", full, "card_draw", "deck_full")

	if n := fx.usage("wizard"); n != 0 {
		t.Errorf("metered %d calls for answers that skipped the model", n)
	}
}

// @spec WIZ-073, WIZ-074, WIZ-076
func TestWizardPropose_RolesPoolHoldsOnlyCardsThatFillTheRole(t *testing.T) {
	fx := newWizFixture(t)
	deckID, _ := fx.deck([]string{"W"}, 2, "midrange_value")

	inRock := fx.roleCard("Pool Rock", "Artifact", rampText, "W")
	inFetch := fx.roleCard("Pool Fetch", "Sorcery", "Search your library for a basic land card, put it onto the battlefield tapped, then shuffle.", "W")
	draw := fx.roleCard("Pool Draw", "Sorcery", drawText, "W")
	removal := fx.roleCard("Pool Removal", "Instant", spotText, "W")
	wipe := fx.roleCard("Pool Wipe", "Sorcery", wipeText, "W")
	vanilla := fx.roleCard("Pool Vanilla", "Creature — Test", plainText, "W")
	land := fx.card(wizCardSpec{name: "Pool Land", typeLine: "Land", text: "{T}: Add {W}.", identity: []string{"W"}, produced: []string{"W"}, usd: f64(1)})
	offColour := fx.roleCard("Pool Off Colour", "Artifact", rampText, "U")
	banned := fx.card(wizCardSpec{name: "Pool Banned", typeLine: "Artifact", text: rampText, identity: []string{"W"}, legal: "banned", usd: f64(1)})
	onDeck := fx.roleCard("Pool On Deck", "Artifact", rampText, "W")
	rejected := fx.roleCard("Pool Rejected", "Artifact", rampText, "W")
	pricey := fx.card(wizCardSpec{name: "Pool Pricey", typeLine: "Artifact", text: rampText, identity: []string{"W"}, usd: f64(90)})
	fx.add(deckID, onDeck, "maybe", 1)
	fx.add(deckID, fx.roleCard("Pool Existing Rock", "Artifact", rampText, "W"), "main", 4)

	var seen ai.CurateRequest
	fx.assistant(capture(&seen))
	got := fx.propose(deckID, map[string]any{
		"step": "roles", "role": "ramp",
		"rejected_card_ids": []string{uuidString(rejected.ID)},
		"restrictions":      map[string]any{"max_price_usd": 10},
	})

	pool := poolNames(seen.Pool)
	wantIn(t, pool, "ramp pool", inRock)
	wantIn(t, pool, "ramp pool", inFetch)
	for label, c := range map[string]wizCard{
		"draw": draw, "removal": removal, "wipe": wipe, "vanilla": vanilla, "land": land, "off-colour": offColour,
		"banned": banned, "on deck": onDeck, "rejected": rejected, "over the price cap": pricey,
	} {
		wantOut(t, pool, "ramp pool/"+label, c)
	}
	if got.Role != "ramp" || got.Source != "popular" || got.Synergy != nil {
		t.Errorf("role=%q source=%q synergy=%v, want ramp/popular/none", got.Role, got.Source, got.Synergy)
	}
	// Four ramp cards against a 10-12 range leave room for 8: the round's cap.
	if seen.Step != "roles" || seen.Want != 8 {
		t.Errorf("step=%q want=%d, want roles/8", seen.Step, seen.Want)
	}
	for _, want := range []string{"ramp", "4", "10", "12"} {
		if !strings.Contains(strings.ToLower(seen.StepGuidance), want) {
			t.Errorf("guidance %q lacks %q", seen.StepGuidance, want)
		}
	}
	if len(got.Proposals) != 1 || !pool[got.Proposals[0].Card.Name] {
		t.Errorf("proposals = %v, want the one pool card the model picked", proposalNames(got.Proposals))
	}
	if n := fx.usage("wizard"); n != 1 {
		t.Errorf("metered %d calls, want 1", n)
	}
}

// @spec WIZ-073
func TestWizardPropose_RolesAskForNoMoreThanTheRoomLeft(t *testing.T) {
	fx := newWizFixture(t)
	deckID, _ := fx.deck([]string{"W"}, 3, "midrange_value")
	fx.roleCard("Room Rock", "Artifact", rampText, "W")
	existing := fx.roleCard("Room Existing", "Artifact", rampText, "W")
	fx.add(deckID, existing, "main", 10) // 10 of 10-12: two slots under the maximum

	var seen ai.CurateRequest
	fx.assistant(capture(&seen))
	fx.propose(deckID, map[string]any{"step": "roles", "role": "ramp"})
	if seen.Want != 2 {
		t.Errorf("want = %d, want 2", seen.Want)
	}
}

// @spec WIZ-075
func TestWizardPropose_RolesDropWhatTheModelNamesOutsideThePool(t *testing.T) {
	fx := newWizFixture(t)
	deckID, _ := fx.deck([]string{"W"}, 3, "midrange_value")
	rock := fx.roleCard("Drop Rock", "Artifact", rampText, "W")
	draw := fx.roleCard("Drop Draw", "Sorcery", drawText, "W") // a real card, but not ramp
	fx.assistant(pickNames(rock.Name, draw.Name, "Nonexistent Card "+fx.suffix))

	got := fx.propose(deckID, map[string]any{"step": "roles", "role": "ramp"})
	names := proposalNames(got.Proposals)
	if len(names) != 1 || names[0] != rock.Name {
		t.Errorf("proposals = %v, want only %q", names, rock.Name)
	}
	if got.Dropped != 2 {
		t.Errorf("dropped = %d, want 2", got.Dropped)
	}
}

// @spec WIZ-074
func TestWizardPropose_RolesHonourTheTutorBanAndGameChangerBudget(t *testing.T) {
	fx := newWizFixture(t)
	tutorTag := fx.tag("tutor", nil)

	deckID, _ := fx.deck([]string{"W"}, 2, "midrange_value") // Bracket 2: no Game Changers
	tutorRock := fx.roleCard("Gate Tutor Rock", "Artifact", rampText+" Search your library for a card.", "W")
	fx.tagCard(tutorTag, tutorRock)
	gcRock := fx.card(wizCardSpec{name: "Gate GC Rock", typeLine: "Artifact", text: rampText, identity: []string{"W"}, gc: true, usd: f64(1)})
	plain := fx.roleCard("Gate Plain Rock", "Artifact", rampText, "W")

	var seen ai.CurateRequest
	fx.assistant(capture(&seen))
	fx.propose(deckID, map[string]any{"step": "roles", "role": "ramp", "restrictions": map[string]any{"no_tutors": true}})
	pool := poolNames(seen.Pool)
	wantIn(t, pool, "tutor-free pool", plain)
	wantOut(t, pool, "tutor-free pool/tutor", tutorRock)
	wantOut(t, pool, "bracket 2/game changer", gcRock)

	b4, _ := fx.deck([]string{"W"}, 4, "midrange_value") // Bracket 4: unlimited
	fx.propose(b4, map[string]any{"step": "roles", "role": "ramp"})
	wantIn(t, poolNames(seen.Pool), "bracket 4 pool", gcRock)
}

// @spec WIZ-074, WIZ-076
func TestWizardPropose_RolesPutSynergyCardsFirstWithAttribution(t *testing.T) {
	fx := newWizFixture(t)
	withSynergy(t, fx.a, true)
	deckID, cmd := fx.deck([]string{"W"}, 3, "midrange_value")
	cleanupSynergy(t, fx.a, cmd.Oracle)

	// "Syn Rock" ranks below the popular rock in edhrec terms but is in the sample.
	popular := fx.roleCard("Role Popular Rock", "Artifact", rampText, "W")
	synRock := fx.roleCard("Role Syn Rock", "Artifact", rampText, "W")
	synDraw := fx.roleCard("Role Syn Draw", "Sorcery", drawText, "W") // in the sample, but not ramp
	if _, err := fx.a.Pool.Exec(context.Background(), `UPDATE cards SET edhrec_rank = 5000 WHERE id = $1`, synRock.ID); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	fetched := pgtype.Timestamptz{Time: time.Now().UTC().Truncate(time.Second), Valid: true}
	if err := fx.a.Queries.UpsertSynergyCommander(ctx, db.UpsertSynergyCommanderParams{
		CommanderOracleID: cmd.Oracle, CommanderName: cmd.Name, ColorIdentity: "W", DeckCount: 38, FetchedAt: fetched,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.a.Queries.InsertSynergyCommanderCards(ctx, []db.InsertSynergyCommanderCardsParams{
		{CommanderOracleID: cmd.Oracle, CardOracleID: synRock.Oracle, DeckCount: 30, InclusionRate: 0.8},
		{CommanderOracleID: cmd.Oracle, CardOracleID: synDraw.Oracle, DeckCount: 30, InclusionRate: 0.8},
	}); err != nil {
		t.Fatal(err)
	}

	var seen ai.CurateRequest
	fx.assistant(capture(&seen))
	got := fx.propose(deckID, map[string]any{"step": "roles", "role": "ramp"})

	if len(seen.Pool) < 2 || seen.Pool[0].Name != synRock.Name || seen.Pool[0].InclusionPct == nil || *seen.Pool[0].InclusionPct != 80 {
		t.Fatalf("pool head = %+v, want the sample's rock first", seen.Pool)
	}
	wantIn(t, poolNames(seen.Pool), "roles pool", popular)
	wantOut(t, poolNames(seen.Pool), "roles pool/non-ramp sample card", synDraw)
	if got.Source != "synergy" || got.Synergy == nil || got.Synergy.Status != "ready" ||
		got.Synergy.Attribution["text"] != "Synergy data from public Archidekt decks" || got.Synergy.Attribution["url"] != "https://archidekt.com" {
		t.Errorf("source=%q synergy=%+v", got.Source, got.Synergy)
	}
	if len(got.Proposals) != 1 || got.Proposals[0].Synergy == nil || got.Proposals[0].Synergy.Decks != 30 {
		t.Errorf("proposal synergy = %+v", got.Proposals)
	}

	// A sample holding no ramp card does not turn the answer into a synergy one.
	other, otherCmd := fx.deck([]string{"W"}, 3, "midrange_value")
	cleanupSynergy(t, fx.a, otherCmd.Oracle)
	if err := fx.a.Queries.UpsertSynergyCommander(ctx, db.UpsertSynergyCommanderParams{
		CommanderOracleID: otherCmd.Oracle, CommanderName: otherCmd.Name, ColorIdentity: "W", DeckCount: 10, FetchedAt: fetched,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.a.Queries.InsertSynergyCommanderCards(ctx, []db.InsertSynergyCommanderCardsParams{
		{CommanderOracleID: otherCmd.Oracle, CardOracleID: synDraw.Oracle, DeckCount: 9, InclusionRate: 0.9},
	}); err != nil {
		t.Fatal(err)
	}
	if got := fx.propose(other, map[string]any{"step": "roles", "role": "ramp"}); got.Source != "popular" || got.Synergy != nil {
		t.Errorf("no ramp in the sample: source=%q synergy=%+v, want popular/none", got.Source, got.Synergy)
	}
}
