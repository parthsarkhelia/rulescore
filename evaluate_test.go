package rulescore

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
)

// rule builds a single-rule ruleset with weight 1, so that Result.Score is 1
// when the rule matches and 0 otherwise.
func rule(op Operator, field string, value any) Ruleset {
	return Ruleset{
		Version: 1,
		Rules:   []Rule{{ID: "r", Field: field, Op: op, Value: value, Weight: 1}},
	}
}

// TestEvaluateRuleOutcomes covers both branches of decisions 1-4: for each
// ambiguous comparison, the case that is evaluated and the case that is not.
func TestEvaluateRuleOutcomes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		ruleset     Ruleset
		record      map[string]any
		wantMatched bool
		wantErr     bool  // the rule could not be evaluated
		wantErrIs   error // sentinel the error must wrap, when there is one
	}{
		// Decision 1: missing field.
		{
			name:        "field present is evaluated",
			ruleset:     rule(OperatorEqual, "status", "active"),
			record:      map[string]any{"status": "active"},
			wantMatched: true,
		},
		{
			name:    "missing field is an evaluation error",
			ruleset: rule(OperatorEqual, "status", "active"),
			record:  map[string]any{"other": "active"},
			wantErr: true, wantErrIs: ErrFieldMissing,
		},
		{
			name:    "missing field does not make ne true",
			ruleset: rule(OperatorNotEqual, "status", "active"),
			record:  map[string]any{},
			wantErr: true, wantErrIs: ErrFieldMissing,
		},

		// Decision 2: type mismatch.
		{
			name:        "same kind compares",
			ruleset:     rule(OperatorGreaterThan, "age", json.Number("18")),
			record:      map[string]any{"age": float64(21)},
			wantMatched: true,
		},
		{
			name:    "string record against numeric rule is an error",
			ruleset: rule(OperatorGreaterThan, "age", json.Number("18")),
			record:  map[string]any{"age": "21"},
			wantErr: true, wantErrIs: ErrTypeMismatch,
		},
		{
			name:    "bool record against string rule is an error under eq",
			ruleset: rule(OperatorEqual, "flag", "true"),
			record:  map[string]any{"flag": true},
			wantErr: true, wantErrIs: ErrTypeMismatch,
		},
		{
			name:    "type mismatch under ne is an error, not true",
			ruleset: rule(OperatorNotEqual, "flag", true),
			record:  map[string]any{"flag": "true"},
			wantErr: true, wantErrIs: ErrTypeMismatch,
		},
		{
			name:    "int record is not recognised as a number",
			ruleset: rule(OperatorEqual, "age", json.Number("21")),
			record:  map[string]any{"age": 21},
			wantErr: true, wantErrIs: ErrTypeMismatch,
		},

		// Decision 3: ordering on non-numerics.
		{
			name:        "strings order lexicographically when greater",
			ruleset:     rule(OperatorGreaterThan, "tier", "gold"),
			record:      map[string]any{"tier": "silver"},
			wantMatched: true,
		},
		{
			name:        "strings order lexicographically when not greater",
			ruleset:     rule(OperatorGreaterThan, "tier", "silver"),
			record:      map[string]any{"tier": "gold"},
			wantMatched: false,
		},
		{
			name:        "string lte is inclusive",
			ruleset:     rule(OperatorLessThanOrEqual, "day", "2026-09-20"),
			record:      map[string]any{"day": "2026-09-20"},
			wantMatched: true,
		},
		{
			name:        "bools compare with eq",
			ruleset:     rule(OperatorEqual, "enabled", true),
			record:      map[string]any{"enabled": true},
			wantMatched: true,
		},
		{
			name:    "bools reject ordering",
			ruleset: rule(OperatorGreaterThan, "enabled", true),
			record:  map[string]any{"enabled": false},
			wantErr: true, wantErrIs: ErrNotOrdered,
		},

		// Decision 4: numeric comparison across representations.
		{
			name:        "float64 record equals json.Number rule",
			ruleset:     rule(OperatorEqual, "age", json.Number("3")),
			record:      map[string]any{"age": float64(3)},
			wantMatched: true,
		},
		{
			name:        "json.Number record equals json.Number rule",
			ruleset:     rule(OperatorEqual, "age", json.Number("3.5")),
			record:      map[string]any{"age": json.Number("3.50")},
			wantMatched: true,
		},
		{
			name:        "exponent notation compares numerically",
			ruleset:     rule(OperatorGreaterThan, "size", json.Number("999")),
			record:      map[string]any{"size": json.Number("1e3")},
			wantMatched: true,
		},
		{
			name:    "NaN cannot be ordered",
			ruleset: rule(OperatorGreaterThan, "score", json.Number("1")),
			record:  map[string]any{"score": math.NaN()},
			wantErr: true,
		},
		{
			name:    "infinity cannot be ordered",
			ruleset: rule(OperatorGreaterThan, "score", json.Number("1")),
			record:  map[string]any{"score": math.Inf(1)},
			wantErr: true,
		},

		// Operator validity on a hand-built ruleset.
		{
			name:    "unknown operator is reported",
			ruleset: rule(Operator("contains"), "tier", "gold"),
			record:  map[string]any{"tier": "gold"},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := tc.ruleset.Evaluate(tc.record)
			if len(got.Rules) != 1 {
				t.Fatalf("Evaluate() breakdown = %d entries, want 1", len(got.Rules))
			}
			entry := got.Rules[0]

			if entry.Matched != tc.wantMatched {
				t.Errorf("Matched = %v, want %v (err = %v)", entry.Matched, tc.wantMatched, entry.Err)
			}
			switch {
			case tc.wantErr && entry.Err == nil:
				t.Errorf("Err = nil, want an evaluation error")
			case !tc.wantErr && entry.Err != nil:
				// The comparison was performed; its outcome is Matched alone.
				t.Errorf("Err = %v, want nil", entry.Err)
			}
			if tc.wantErrIs != nil && !errors.Is(entry.Err, tc.wantErrIs) {
				t.Errorf("Err = %v, want errors.Is(_, %v)", entry.Err, tc.wantErrIs)
			}

			wantScore := 0.0
			if tc.wantMatched {
				wantScore = 1
			}
			if got.Score != wantScore {
				t.Errorf("Score = %v, want %v", got.Score, wantScore)
			}
		})
	}
}

// TestEvaluateUnevaluableRulesAreDistinctFromNonMatches pins decision 1's
// requirement that a caller can tell the two apart.
func TestEvaluateUnevaluableRulesAreDistinctFromNonMatches(t *testing.T) {
	t.Parallel()

	rs := Ruleset{Version: 1, Rules: []Rule{
		{ID: "missing", Field: "absent", Op: OperatorEqual, Value: "x", Weight: 1},
		{ID: "no_match", Field: "tier", Op: OperatorEqual, Value: "gold", Weight: 1},
	}}

	got := rs.Evaluate(map[string]any{"tier": "silver"})

	if got.Rules[0].Err == nil {
		t.Errorf("missing field: Err = nil, want an error")
	}
	if got.Rules[1].Err != nil {
		t.Errorf("evaluated non-match: Err = %v, want nil", got.Rules[1].Err)
	}
	for _, entry := range got.Rules {
		if entry.Matched {
			t.Errorf("rule %q matched, want false", entry.ID)
		}
	}
}

// TestEvaluateLargeIntegerPrecision is the reason rules carry json.Number:
// these values are indistinguishable once converted to float64.
func TestEvaluateLargeIntegerPrecision(t *testing.T) {
	t.Parallel()

	const (
		lower  json.Number = "9007199254740992" // 2^53, the last exactly representable float64 integer
		higher json.Number = "9007199254740993" // 2^53+1, which float64 rounds back down to 2^53
	)

	tests := []struct {
		name   string
		op     Operator
		record json.Number // record side
		value  json.Number // rule side
		want   bool
	}{
		{name: "2^53+1 is greater than 2^53", op: OperatorGreaterThan, record: higher, value: lower, want: true},
		{name: "2^53+1 does not equal 2^53", op: OperatorEqual, record: higher, value: lower, want: false},
		{name: "2^53 equals 2^53", op: OperatorEqual, record: lower, value: lower, want: true},
		{name: "2^53 is not greater than 2^53", op: OperatorGreaterThan, record: lower, value: lower, want: false},
		{name: "20-digit values stay distinct", op: OperatorGreaterThan, record: "12345678901234567891", value: "12345678901234567890", want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := rule(tc.op, "n", tc.value).Evaluate(map[string]any{"n": tc.record})
			if got.Rules[0].Err != nil {
				t.Fatalf("Err = %v, want nil", got.Rules[0].Err)
			}
			if got.Rules[0].Matched != tc.want {
				t.Errorf("Matched = %v, want %v", got.Rules[0].Matched, tc.want)
			}
		})
	}
}

// TestEvaluateScore covers decision 5, including the zero-total-weight case.
func TestEvaluateScore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		ruleset   Ruleset
		record    map[string]any
		wantScore float64
		wantRules int
	}{
		{
			name:      "empty ruleset scores zero",
			ruleset:   Ruleset{Version: 1},
			record:    map[string]any{"age": float64(21)},
			wantScore: 0,
			wantRules: 0,
		},
		{
			name: "score is matched weight over total weight",
			ruleset: Ruleset{Version: 1, Rules: []Rule{
				{ID: "a", Field: "age", Op: OperatorGreaterThanOrEqual, Value: json.Number("18"), Weight: 3},
				{ID: "b", Field: "tier", Op: OperatorEqual, Value: "gold", Weight: 1},
			}},
			record:    map[string]any{"age": float64(21), "tier": "silver"},
			wantScore: 0.75,
			wantRules: 2,
		},
		{
			name: "all rules matched scores one",
			ruleset: Ruleset{Version: 1, Rules: []Rule{
				{ID: "a", Field: "age", Op: OperatorGreaterThanOrEqual, Value: json.Number("18"), Weight: 3},
				{ID: "b", Field: "tier", Op: OperatorEqual, Value: "gold", Weight: 1},
			}},
			record:    map[string]any{"age": float64(21), "tier": "gold"},
			wantScore: 1,
			wantRules: 2,
		},
		{
			name: "unevaluable rules stay in the denominator",
			ruleset: Ruleset{Version: 1, Rules: []Rule{
				{ID: "a", Field: "age", Op: OperatorGreaterThanOrEqual, Value: json.Number("18"), Weight: 1},
				{ID: "b", Field: "absent", Op: OperatorEqual, Value: "gold", Weight: 1},
			}},
			record:    map[string]any{"age": float64(21)},
			wantScore: 0.5,
			wantRules: 2,
		},
		{
			name: "zero total weight scores zero, not NaN",
			ruleset: Ruleset{Version: 1, Rules: []Rule{
				{ID: "a", Field: "age", Op: OperatorGreaterThanOrEqual, Value: json.Number("18"), Weight: 0},
				{ID: "b", Field: "tier", Op: OperatorEqual, Value: "gold", Weight: 0},
			}},
			record:    map[string]any{"age": float64(21), "tier": "gold"},
			wantScore: 0,
			wantRules: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := tc.ruleset.Evaluate(tc.record)
			if math.IsNaN(got.Score) {
				t.Fatalf("Score = NaN, want a number")
			}
			if got.Score != tc.wantScore {
				t.Errorf("Score = %v, want %v", got.Score, tc.wantScore)
			}
			if len(got.Rules) != tc.wantRules {
				t.Fatalf("breakdown = %d entries, want %d", len(got.Rules), tc.wantRules)
			}
			for i, entry := range got.Rules {
				if entry.ID != tc.ruleset.Rules[i].ID {
					t.Errorf("breakdown[%d].ID = %q, want %q", i, entry.ID, tc.ruleset.Rules[i].ID)
				}
				if entry.Weight != tc.ruleset.Rules[i].Weight {
					t.Errorf("breakdown[%d].Weight = %v, want %v", i, entry.Weight, tc.ruleset.Rules[i].Weight)
				}
			}
		})
	}
}

// TestEvaluateDecodedRuleset checks evaluation against the representation
// Decode actually produces, rather than only hand-built rulesets.
func TestEvaluateDecodedRuleset(t *testing.T) {
	t.Parallel()

	rs, err := Decode([]byte(`{
		"version": 1,
		"rules": [
			{"id":"age_ok","field":"age","op":"gte","value":18,"weight":2},
			{"id":"verified","field":"status","op":"eq","value":"active","weight":1},
			{"id":"enabled","field":"enabled","op":"eq","value":true,"weight":1}
		]
	}`))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	var record map[string]any
	if err := json.Unmarshal([]byte(`{"age":21,"status":"active","enabled":false}`), &record); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	got := rs.Evaluate(record)
	if got.Score != 0.75 {
		t.Errorf("Score = %v, want 0.75", got.Score)
	}
	for _, entry := range got.Rules {
		if entry.Err != nil {
			t.Errorf("rule %q: Err = %v, want nil", entry.ID, entry.Err)
		}
	}
	if !got.Rules[0].Matched || !got.Rules[1].Matched || got.Rules[2].Matched {
		t.Errorf("breakdown = %+v, want age_ok and verified matched, enabled not", got.Rules)
	}
}
