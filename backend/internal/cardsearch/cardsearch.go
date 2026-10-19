// Package cardsearch parses the subset of Scryfall query syntax Manafold
// supports over its local mirror, and builds the SQL WHERE clause for it. It is
// deliberately separate from the HTTP handler and unit-tested on its own,
// because the query grammar grows and the parse step is where a
// plausible-but-wrong query would silently return the wrong cards.
//
// @spec CARD-022, CARD-023, CARD-064
package cardsearch

import (
	"fmt"
	"strconv"
	"strings"
)

// Query is a parsed search query: every field ANDs with the others.
type Query struct {
	// FullText is the combined bare-word terms, fed to plainto_tsquery.
	FullText string
	// OraclePhrases are o:"..." predicates matched as substrings of oracle_text.
	OraclePhrases []string
	// TypeTerms are t:... predicates matched as substrings of type_line.
	TypeTerms []string
	// ColorIdentity, when set, constrains color_identity.
	ColorIdentity *ColorPredicate
	// ManaValue holds cmc comparisons, all ANDed.
	ManaValue []NumPredicate
	// CommanderOnly is set by is:commander.
	CommanderOnly bool
	// OracleTags are otag:<slug> predicates (lowercased): the card carries the
	// tag or any of its descendants (CARD-064).
	OracleTags []string
}

// ColorPredicate constrains a card's color_identity. Op is one of
// "subset" (id: / id<=), "exact" (id=), "superset" (id>=). Colors is the
// WUBRG letters (uppercase); a colourless-only query yields an empty slice.
type ColorPredicate struct {
	Colors []string
	Op     string
}

// NumPredicate is one numeric comparison. Op is one of "<=", ">=", "=", "<", ">".
type NumPredicate struct {
	Op    string
	Value float64
}

// ParseError names the token that could not be parsed, so the handler can
// return 400 with a message pointing at it (CARD-023).
type ParseError struct {
	Token  string
	Reason string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("cannot parse query token %q: %s", e.Token, e.Reason)
}

var colorLetters = map[rune]string{
	'w': "W", 'u': "U", 'b': "B", 'r': "R", 'g': "G",
}

// Parse turns a raw query string into a Query. It returns a *ParseError for any
// token it does not recognise, rather than dropping it silently.
func Parse(raw string) (Query, error) {
	var q Query
	var fullText []string

	for _, tok := range tokenize(raw) {
		if tok == "" {
			continue
		}
		key, op, val, isField := splitToken(tok)
		if !isField {
			fullText = append(fullText, tok)
			continue
		}
		switch key {
		case "id", "identity", "ci":
			cp, err := parseColorPredicate(op, val, tok)
			if err != nil {
				return Query{}, err
			}
			q.ColorIdentity = cp
		case "t", "type":
			if val == "" {
				return Query{}, &ParseError{Token: tok, Reason: "empty type term"}
			}
			q.TypeTerms = append(q.TypeTerms, val)
		case "o", "oracle":
			if val == "" {
				return Query{}, &ParseError{Token: tok, Reason: "empty oracle term"}
			}
			q.OraclePhrases = append(q.OraclePhrases, val)
		case "cmc", "mv":
			np, err := parseNumPredicate(op, val, tok)
			if err != nil {
				return Query{}, err
			}
			q.ManaValue = append(q.ManaValue, np)
		case "otag", "oracletag":
			if op != ":" && op != "=" {
				return Query{}, &ParseError{Token: tok, Reason: "otag supports : and = only"}
			}
			if val == "" {
				return Query{}, &ParseError{Token: tok, Reason: "empty oracle tag"}
			}
			q.OracleTags = append(q.OracleTags, strings.ToLower(val))
		case "is":
			if strings.EqualFold(val, "commander") {
				q.CommanderOnly = true
			} else {
				return Query{}, &ParseError{Token: tok, Reason: "unsupported is: value (only is:commander)"}
			}
		default:
			return Query{}, &ParseError{Token: tok, Reason: "unknown search key"}
		}
	}

	q.FullText = strings.Join(fullText, " ")
	return q, nil
}

// tokenize splits on whitespace while keeping "quoted phrases" (including a
// key:"quoted value") as a single token.
func tokenize(raw string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for _, r := range raw {
		switch {
		case r == '"':
			inQuote = !inQuote
		case (r == ' ' || r == '\t' || r == '\n') && !inQuote:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// splitToken parses "key:value", "key=value", "key<=value", "key>=value",
// "key<value", "key>value". isField is false for a bare word.
func splitToken(tok string) (key, op, val string, isField bool) {
	for i, r := range tok {
		switch r {
		case ':':
			return strings.ToLower(tok[:i]), ":", tok[i+1:], i > 0
		case '=':
			return strings.ToLower(tok[:i]), "=", tok[i+1:], i > 0
		case '<', '>':
			if i+1 < len(tok) && tok[i+1] == '=' {
				return strings.ToLower(tok[:i]), string(r) + "=", tok[i+2:], i > 0
			}
			return strings.ToLower(tok[:i]), string(r), tok[i+1:], i > 0
		}
	}
	return "", "", tok, false
}

func parseColorPredicate(op, val, tok string) (*ColorPredicate, error) {
	var cp ColorPredicate
	switch op {
	case ":", "<=":
		cp.Op = "subset"
	case "=":
		cp.Op = "exact"
	case ">=":
		cp.Op = "superset"
	default:
		return nil, &ParseError{Token: tok, Reason: "id: supports :, =, <=, >= only"}
	}
	seen := map[string]bool{}
	for _, r := range strings.ToLower(val) {
		if r == 'c' {
			continue // colourless contributes no letter
		}
		letter, ok := colorLetters[r]
		if !ok {
			return nil, &ParseError{Token: tok, Reason: fmt.Sprintf("%q is not a WUBRG colour letter", string(r))}
		}
		if !seen[letter] {
			seen[letter] = true
			cp.Colors = append(cp.Colors, letter)
		}
	}
	return &cp, nil
}

func parseNumPredicate(op, val, tok string) (NumPredicate, error) {
	switch op {
	case ":":
		op = "="
	case "=", "<=", ">=", "<", ">":
	default:
		return NumPredicate{}, &ParseError{Token: tok, Reason: "cmc supports :, =, <=, >=, <, > only"}
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
	if err != nil {
		return NumPredicate{}, &ParseError{Token: tok, Reason: "cmc value is not a number"}
	}
	return NumPredicate{Op: op, Value: n}, nil
}

// WhereSQL renders the query as a SQL boolean expression over a "cards c" alias,
// with placeholders numbered from startParam. It returns the expression, the
// ordered argument list, and the next free placeholder index. An empty query
// renders as "TRUE".
func (q Query) WhereSQL(startParam int) (expr string, args []any, next int) {
	p := startParam
	var clauses []string
	add := func(format string, a ...any) {
		nums := make([]any, len(a))
		for i := range a {
			nums[i] = p
			p++
		}
		clauses = append(clauses, fmt.Sprintf(format, nums...))
		args = append(args, a...)
	}

	if strings.TrimSpace(q.FullText) != "" {
		add("to_tsvector('english', c.name || ' ' || c.oracle_text || ' ' || c.type_line) @@ plainto_tsquery('english', $%d)", q.FullText)
	}
	for _, phrase := range q.OraclePhrases {
		add("c.oracle_text ILIKE '%%' || $%d || '%%'", phrase)
	}
	for _, t := range q.TypeTerms {
		add("c.type_line ILIKE '%%' || $%d || '%%'", t)
	}
	if q.ColorIdentity != nil {
		colors := q.ColorIdentity.Colors
		if colors == nil {
			colors = []string{}
		}
		switch q.ColorIdentity.Op {
		case "subset":
			add("c.color_identity <@ $%d::text[]", colors)
		case "superset":
			add("$%d::text[] <@ c.color_identity", colors)
		case "exact":
			add("(c.color_identity <@ $%d::text[] AND $%d::text[] <@ c.color_identity)", colors, colors)
		}
	}
	for _, np := range q.ManaValue {
		add("c.mana_value "+np.Op+" $%d", np.Value)
	}
	if q.CommanderOnly {
		clauses = append(clauses, "c.can_be_commander = true")
	}
	// The tag or any descendant, through the materialised closure; a disabled
	// tag matches nothing and cuts off every tag below it — the pair is dropped
	// when a disabled tag x lies on it (closure(a, x) and closure(x, d))
	// (CARD-062, CARD-063).
	for _, slug := range q.OracleTags {
		add(`EXISTS (SELECT 1 FROM oracle_tags a
			JOIN oracle_tag_closure cl ON cl.ancestor_id = a.id
			JOIN card_oracle_tags ct ON ct.tag_id = cl.descendant_id
			WHERE a.slug = $%d AND ct.card_id = c.id
			AND NOT EXISTS (SELECT 1 FROM oracle_tag_closure up
				JOIN oracle_tags x ON x.id = up.descendant_id AND x.disabled
				JOIN oracle_tag_closure down ON down.ancestor_id = x.id
				WHERE up.ancestor_id = a.id AND down.descendant_id = cl.descendant_id))`, slug)
	}

	if len(clauses) == 0 {
		return "TRUE", args, p
	}
	return strings.Join(clauses, " AND "), args, p
}
