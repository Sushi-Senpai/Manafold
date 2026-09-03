package cardsearch

import (
	"reflect"
	"strings"
	"testing"
)

// @spec CARD-022
func TestParse_Predicates(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want Query
	}{
		{
			name: "bare words become full text",
			raw:  "draw a card",
			want: Query{FullText: "draw a card"},
		},
		{
			name: "color identity subset via id:",
			raw:  "id:wug",
			want: Query{ColorIdentity: &ColorPredicate{Colors: []string{"W", "U", "G"}, Op: "subset"}},
		},
		{
			name: "color identity subset via id<=",
			raw:  "id<=gr",
			want: Query{ColorIdentity: &ColorPredicate{Colors: []string{"G", "R"}, Op: "subset"}},
		},
		{
			name: "color identity exact via id=",
			raw:  "id=b",
			want: Query{ColorIdentity: &ColorPredicate{Colors: []string{"B"}, Op: "exact"}},
		},
		{
			name: "colourless id:c yields empty colours",
			raw:  "id:c",
			want: Query{ColorIdentity: &ColorPredicate{Colors: nil, Op: "subset"}},
		},
		{
			name: "type and cmc",
			raw:  "t:creature cmc<=3",
			want: Query{TypeTerms: []string{"creature"}, ManaValue: []NumPredicate{{Op: "<=", Value: 3}}},
		},
		{
			name: "cmc colon means equals",
			raw:  "cmc:2",
			want: Query{ManaValue: []NumPredicate{{Op: "=", Value: 2}}},
		},
		{
			name: "oracle phrase",
			raw:  `o:"draw a card"`,
			want: Query{OraclePhrases: []string{"draw a card"}},
		},
		{
			name: "is:commander",
			raw:  "is:commander",
			want: Query{CommanderOnly: true},
		},
		{
			name: "combined",
			raw:  `sol id:c t:artifact cmc<=1`,
			want: Query{
				FullText:      "sol",
				TypeTerms:     []string{"artifact"},
				ManaValue:     []NumPredicate{{Op: "<=", Value: 1}},
				ColorIdentity: &ColorPredicate{Colors: nil, Op: "subset"},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.raw)
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tc.raw, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Parse(%q)\n got: %#v\nwant: %#v", tc.raw, got, tc.want)
			}
		})
	}
}

// @spec CARD-023
func TestParse_UnparseableTokenErrorsWithToken(t *testing.T) {
	for _, raw := range []string{
		"power:5",   // unknown key
		"id:xyz",    // x is not a colour letter
		"cmc<=lots", // non-numeric cmc
		"is:banned", // unsupported is: value
		"id>b",      // id does not support bare >
	} {
		t.Run(raw, func(t *testing.T) {
			_, err := Parse(raw)
			if err == nil {
				t.Fatalf("Parse(%q) expected a parse error, got nil", raw)
			}
			var pe *ParseError
			if !asParseError(err, &pe) {
				t.Fatalf("Parse(%q) error is not *ParseError: %T", raw, err)
			}
			if pe.Token == "" || !strings.Contains(err.Error(), pe.Token) {
				t.Fatalf("Parse(%q) error %q does not name the offending token", raw, err)
			}
		})
	}
}

func asParseError(err error, target **ParseError) bool {
	pe, ok := err.(*ParseError)
	if ok {
		*target = pe
	}
	return ok
}

// @spec CARD-021, CARD-022
func TestWhereSQL_RendersPlaceholdersAndArgs(t *testing.T) {
	q, err := Parse(`bolt t:instant cmc<=1 id:r`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	expr, args, next := q.WhereSQL(1)
	if next != 5 {
		t.Fatalf("expected next placeholder 5, got %d (expr=%q)", next, expr)
	}
	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d: %#v", len(args), args)
	}
	for _, frag := range []string{"plainto_tsquery", "type_line ILIKE", "mana_value <= $", "color_identity <@ $"} {
		if !strings.Contains(expr, frag) {
			t.Fatalf("expr %q missing fragment %q", expr, frag)
		}
	}
}

func TestWhereSQL_EmptyQueryIsTrue(t *testing.T) {
	q, _ := Parse("")
	expr, args, _ := q.WhereSQL(1)
	if expr != "TRUE" || len(args) != 0 {
		t.Fatalf("empty query: expr=%q args=%#v", expr, args)
	}
}
