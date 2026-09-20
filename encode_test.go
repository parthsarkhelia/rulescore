package rulescore

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestEncodeRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		json string
	}{
		{
			name: "all value kinds and exact decimals",
			json: `{"version":1,"rules":[` +
				`{"id":"large_integer","field":"integer","op":"eq","value":9007199254740993,"weight":1},` +
				`{"id":"many_digits","field":"precise","op":"gte","value":12345678901234567890.12345678901234567890,"weight":0.25},` +
				`{"id":"not_float_shortest","field":"decimal","op":"lt","value":0.10000000000000001,"weight":0.5},` +
				`{"id":"text","field":"status","op":"ne","value":"inactive","weight":0},` +
				`{"id":"boolean","field":"enabled","op":"eq","value":true,"weight":2}` +
				`]}`,
		},
		{name: "nil rules", json: `{"version":1,"rules":null}`},
		{name: "empty rules", json: `{"version":1,"rules":[]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			want, err := Decode([]byte(tt.json))
			if err != nil {
				t.Fatalf("Decode(input) error = %v", err)
			}

			encoded, err := Encode(want)
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			got, err := Decode(encoded)
			if err != nil {
				t.Fatalf("Decode(Encode()) error = %v", err)
			}

			// DeepEqual checks more than numeric equivalence: json.Number is a
			// string type, so it also requires the original decimal text, and it
			// distinguishes nil Rules from an allocated empty slice.
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Decode(Encode()) = %#v, want %#v", got, want)
			}
		})
	}
}

func TestEncodePreservesExactNumberText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		number json.Number
	}{
		{name: "integer beyond float64 precision", number: "9007199254740993"},
		{name: "more than seventeen significant digits", number: "12345678901234567890.12345678901234567890"},
		{name: "decimal differs from shortest float64 round trip", number: "0.10000000000000001"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rs := Ruleset{Version: 1, Rules: []Rule{{
				ID: "precise", Field: "amount", Op: OperatorEqual, Value: tt.number, Weight: 1,
			}}}
			encoded, err := Encode(rs)
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			if !strings.Contains(string(encoded), `"value":`+tt.number.String()) {
				t.Errorf("Encode() = %s, want exact number %s", encoded, tt.number)
			}

			got, err := Decode(encoded)
			if err != nil {
				t.Fatalf("Decode(Encode()) error = %v", err)
			}
			if gotNumber, ok := got.Rules[0].Value.(json.Number); !ok || gotNumber != tt.number {
				t.Errorf("round-trip number = %#v, want json.Number(%q)", got.Rules[0].Value, tt.number)
			}
		})
	}
}

func TestEncodeRejectsUnencodableValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "integer is not coerced", value: 18, want: "got int"},
		{name: "nil is not emitted", value: nil, want: "got <nil>"},
		{name: "struct is not emitted", value: struct{ Minimum int }{18}, want: "got struct"},
		{name: "slice is not emitted", value: []string{"active"}, want: "got []string"},
		{name: "empty json number", value: json.Number(""), want: "must be a valid JSON number"},
		{name: "malformed json number", value: json.Number("01"), want: "must be a valid JSON number"},
		{name: "invalid UTF-8 string", value: string([]byte{0xff}), want: "must contain valid UTF-8"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rs := Ruleset{Version: 1, Rules: []Rule{{
				ID: "age_ok", Field: "age", Op: OperatorEqual, Value: tt.value, Weight: 1,
			}}}
			_, err := Encode(rs)
			if err == nil {
				t.Fatal("Encode() error = nil, want an error")
			}
			for _, part := range []string{`rule "age_ok"`, `field "value"`, tt.want} {
				if !strings.Contains(err.Error(), part) {
					t.Errorf("Encode() error = %q, want it to contain %q", err, part)
				}
			}
		})
	}
}

func TestEncodeRejectsInvalidRulesets(t *testing.T) {
	t.Parallel()

	validRule := Rule{ID: "age_ok", Field: "age", Op: OperatorGreaterThanOrEqual, Value: json.Number("18"), Weight: 1}
	tests := []struct {
		name      string
		ruleset   Ruleset
		wantParts []string
	}{
		{
			name: "unsupported version", ruleset: Ruleset{Version: 2},
			wantParts: []string{`ruleset field "version"`, "unsupported version 2"},
		},
		{
			name: "empty id", ruleset: Ruleset{Version: 1, Rules: []Rule{{Field: "age", Op: OperatorEqual, Value: json.Number("18"), Weight: 1}}},
			wantParts: []string{`rule "<empty>"`, `field "id"`, "must not be empty"},
		},
		{
			name: "duplicate id", ruleset: Ruleset{Version: 1, Rules: []Rule{validRule, validRule}},
			wantParts: []string{`rule "age_ok"`, `field "id"`, "duplicate id"},
		},
		{
			name: "empty field", ruleset: Ruleset{Version: 1, Rules: []Rule{{ID: "age_ok", Op: OperatorEqual, Value: json.Number("18"), Weight: 1}}},
			wantParts: []string{`rule "age_ok"`, `field "field"`, "must not be empty"},
		},
		{
			name: "invalid UTF-8 id",
			ruleset: Ruleset{Version: 1, Rules: []Rule{{
				ID: string([]byte{0xff}), Field: "age", Op: OperatorEqual, Value: json.Number("18"), Weight: 1,
			}}},
			wantParts: []string{`rule "\xff"`, `field "id"`, "must contain valid UTF-8"},
		},
		{
			name: "invalid UTF-8 field",
			ruleset: Ruleset{Version: 1, Rules: []Rule{{
				ID: "age_ok", Field: string([]byte{0xff}), Op: OperatorEqual, Value: json.Number("18"), Weight: 1,
			}}},
			wantParts: []string{`rule "age_ok"`, `field "field"`, "must contain valid UTF-8"},
		},
		{
			name: "unknown operator", ruleset: Ruleset{Version: 1, Rules: []Rule{{ID: "age_ok", Field: "age", Op: "contains", Value: json.Number("18"), Weight: 1}}},
			wantParts: []string{`rule "age_ok"`, `field "op"`, `unknown operator "contains"`},
		},
		{
			name: "negative weight", ruleset: Ruleset{Version: 1, Rules: []Rule{{ID: "age_ok", Field: "age", Op: OperatorEqual, Value: json.Number("18"), Weight: -0.25}}},
			wantParts: []string{`rule "age_ok"`, `field "weight"`, "must not be negative"},
		},
		{
			name: "NaN weight", ruleset: Ruleset{Version: 1, Rules: []Rule{{ID: "age_ok", Field: "age", Op: OperatorEqual, Value: json.Number("18"), Weight: math.NaN()}}},
			wantParts: []string{`rule "age_ok"`, `field "weight"`, "must be finite"},
		},
		{
			name: "positive infinite weight", ruleset: Ruleset{Version: 1, Rules: []Rule{{ID: "age_ok", Field: "age", Op: OperatorEqual, Value: json.Number("18"), Weight: math.Inf(1)}}},
			wantParts: []string{`rule "age_ok"`, `field "weight"`, "must be finite"},
		},
		{
			name: "negative infinite weight", ruleset: Ruleset{Version: 1, Rules: []Rule{{ID: "age_ok", Field: "age", Op: OperatorEqual, Value: json.Number("18"), Weight: math.Inf(-1)}}},
			wantParts: []string{`rule "age_ok"`, `field "weight"`, "must be finite"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Encode(tt.ruleset)
			if err == nil {
				t.Fatal("Encode() error = nil, want an error")
			}
			for _, part := range tt.wantParts {
				if !strings.Contains(err.Error(), part) {
					t.Errorf("Encode() error = %q, want it to contain %q", err, part)
				}
			}
		})
	}
}

func TestEncodeOutputIsByteStable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rs   Ruleset
		want string
	}{
		{
			name: "declaration order and number spelling are stable",
			rs: Ruleset{Version: 1, Rules: []Rule{
				{ID: "second", Field: "amount", Op: OperatorGreaterThan, Value: json.Number("1.2300e+4"), Weight: 0.5},
				{ID: "first", Field: "active", Op: OperatorEqual, Value: true, Weight: 1},
			}},
			want: `{"version":1,"rules":[{"id":"second","field":"amount","op":"gt","value":1.2300e+4,"weight":0.5},{"id":"first","field":"active","op":"eq","value":true,"weight":1}]}`,
		},
		{name: "nil rule slice", rs: Ruleset{Version: 1}, want: `{"version":1,"rules":null}`},
		{name: "empty rule slice", rs: Ruleset{Version: 1, Rules: []Rule{}}, want: `{"version":1,"rules":[]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			first, err := Encode(tt.rs)
			if err != nil {
				t.Fatalf("first Encode() error = %v", err)
			}
			second, err := Encode(tt.rs)
			if err != nil {
				t.Fatalf("second Encode() error = %v", err)
			}
			if string(first) != tt.want {
				t.Errorf("Encode() = %s, want %s", first, tt.want)
			}
			if !reflect.DeepEqual(second, first) {
				t.Errorf("second Encode() = %s, first = %s", second, first)
			}
		})
	}
}
