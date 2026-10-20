package synergy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"sync"
	"testing"
)

// archidektFixtureServer serves the recorded search pages and decks, and
// records every path requested so tests can assert nothing else was fetched.
type archidektFixtureServer struct {
	*httptest.Server
	mu   sync.Mutex
	hits []string

	// failDecks makes every deck detail answer HTTP 500. tooManyFirst makes the
	// first N requests for each distinct URL answer HTTP 429.
	failDecks    bool
	tooManyFirst int
	perURL       map[string]int
}

// baselineCommanderName is the commander whose search returns the baseline
// fixture page instead of the Krenko one.
const baselineCommanderName = "Synergy Test Baseline Commander"

func newFixtureServer(t *testing.T) *archidektFixtureServer {
	t.Helper()
	fs := &archidektFixtureServer{perURL: make(map[string]int)}
	deckPath := regexp.MustCompile(`^/api/decks/(\d+)/$`)
	fs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fs.mu.Lock()
		fs.hits = append(fs.hits, r.URL.RequestURI())
		fs.perURL[r.URL.RequestURI()]++
		limited := fs.perURL[r.URL.RequestURI()] <= fs.tooManyFirst
		fs.mu.Unlock()
		if limited {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		switch {
		case r.URL.Path == "/api/decks/v3/" && r.URL.Query().Get("commanderName") == baselineCommanderName:
			_, _ = w.Write(fixture(t, "search_baseline.json"))
		case deckPath.MatchString(r.URL.Path) && fs.failDecks:
			w.WriteHeader(http.StatusInternalServerError)
		case r.URL.Path == "/api/decks/v3/" && r.URL.Query().Get("page") == "2":
			_, _ = w.Write(fixture(t, "search_page2.json"))
		case r.URL.Path == "/api/decks/v3/":
			_, _ = w.Write(fixture(t, "search_page1.json"))
		case deckPath.MatchString(r.URL.Path):
			id := deckPath.FindStringSubmatch(r.URL.Path)[1]
			switch id {
			case "101", "106", "107", "109":
				_, _ = w.Write(fixture(t, "deck_"+id+".json"))
			default: // 108 went private between search and fetch
				w.WriteHeader(http.StatusNotFound)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(fs.Close)
	return fs
}

func (fs *archidektFixtureServer) deckRequests() []string {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	re := regexp.MustCompile(`^/api/decks/\d+/$`)
	var out []string
	for _, h := range fs.hits {
		if re.MatchString(h) {
			out = append(out, h)
		}
	}
	return out
}

func krenkoRef() commanderRef {
	return commanderRef{oracle: krenko, name: "Krenko, Mob Boss", identity: "R"}
}

// @spec SYN-002, SYN-005, SYN-010, SYN-012, SYN-013
func TestSampleCommanderDecks_FiltersCandidatesFetchesOnlyThemAndSkipsVanishedDecks(t *testing.T) {
	useFakeClock(t)
	fs := newFixtureServer(t)
	c := newArchidektClient(fs.URL, nil, 50)
	s := newSample()

	if err := sampleCommanderDecks(context.Background(), c, krenkoRef(), 60, 3, s); err != nil {
		t.Fatal(err)
	}

	// Page 1 candidates are 101, 106, 107, 108 — 102 private, 103 unlisted,
	// 104 not Commander, 105 under 98 cards. 109 is the page-2 candidate. 108
	// answers 404 and is skipped. Only search-returned candidates are fetched.
	wantFetched := []string{"/api/decks/101/", "/api/decks/106/", "/api/decks/107/", "/api/decks/108/", "/api/decks/109/"}
	if got := fs.deckRequests(); !reflect.DeepEqual(got, wantFetched) {
		t.Errorf("deck requests = %v, want %v", got, wantFetched)
	}

	// 101 and 107 tally identically; 106 and 109 are distinct.
	if s.decks != 3 || s.duplicates != 1 {
		t.Errorf("decks=%d duplicates=%d, want 3 and 1", s.decks, s.duplicates)
	}
	if got := s.cardDecks[solRing]; got != 3 {
		t.Errorf("Sol Ring in %d decks, want 3", got)
	}
	if got := s.cardDecks[warchief]; got != 2 {
		t.Errorf("Goblin Warchief in %d decks, want 2", got)
	}
	if got := s.cardDecks[skullclamp]; got != 1 {
		t.Errorf("Skullclamp in %d decks, want 1", got)
	}
	if _, ok := s.cardDecks[krenko]; ok {
		t.Error("the sampled commander was tallied as one of its own cards")
	}
}

// @spec SYN-010
func TestSampleCommanderDecks_StopsAtTheConfiguredDeckCount(t *testing.T) {
	useFakeClock(t)
	fs := newFixtureServer(t)
	c := newArchidektClient(fs.URL, nil, 50)
	s := newSample()

	if err := sampleCommanderDecks(context.Background(), c, krenkoRef(), 2, 3, s); err != nil {
		t.Fatal(err)
	}
	want := []string{"/api/decks/101/", "/api/decks/106/"}
	if got := fs.deckRequests(); !reflect.DeepEqual(got, want) {
		t.Errorf("deck requests = %v, want %v", got, want)
	}
	// Reading page 1 filled the sample, so page 2 was never requested.
	for _, h := range fs.hits {
		if regexp.MustCompile(`page=2`).MatchString(h) {
			t.Errorf("fetched search page 2 (%s) though the sample was already full", h)
		}
	}
}

// @spec SYN-010
func TestSampleCommanderDecks_ReadsAtMostTheConfiguredSearchPages(t *testing.T) {
	useFakeClock(t)
	fs := newFixtureServer(t)
	c := newArchidektClient(fs.URL, nil, 50)
	s := newSample()

	if err := sampleCommanderDecks(context.Background(), c, krenkoRef(), 60, 1, s); err != nil {
		t.Fatal(err)
	}
	for _, h := range fs.deckRequests() {
		if h == "/api/decks/109/" {
			t.Error("deck 109 is on search page 2, but only 1 page was allowed")
		}
	}
}
