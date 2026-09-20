package rulescore

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestDecode(t *testing.T) {
	t.Parallel()

	input := []byte(`{
		"version": 1,
		"rules": [
			{"id":"age_ok","field":"age","op":"gte","value":18,"weight":0.5},
			{"id":"verified","field":"status","op":"eq","value":"active","weight":0.5},
			{"id":"enabled","field":"enabled","op":"eq","value":true,"weight":0}
		]
	}`)

	got, err := Decode(input)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	want := Ruleset{
		Version: 1,
		Rules: []Rule{
			{ID: "age_ok", Field: "age", Op: OperatorGreaterThanOrEqual, Value: json.Number("18"), Weight: 0.5},
			{ID: "verified", Field: "status", Op: OperatorEqual, Value: "active", Weight: 0.5},
			{ID: "enabled", Field: "enabled", Op: OperatorEqual, Value: true, Weight: 0},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Decode() = %#v, want %#v", got, want)
	}
}

func TestDecodeRejectsInvalidRulesets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantParts []string
	}{
		{
			name:      "unknown operator",
			input:     `{"version":1,"rules":[{"id":"age_ok","field":"age","op":"contains","value":18,"weight":1}]}`,
			wantParts: []string{`rule "age_ok"`, `field "op"`, `unknown operator "contains"`},
		},
		{
			name:      "empty id",
			input:     `{"version":1,"rules":[{"id":"","field":"age","op":"gte","value":18,"weight":1}]}`,
			wantParts: []string{`rule "<empty>"`, `field "id"`, "must not be empty"},
		},
		{
			name:      "duplicate id",
			input:     `{"version":1,"rules":[{"id":"age_ok","field":"age","op":"gte","value":18,"weight":1},{"id":"age_ok","field":"other_age","op":"lt","value":65,"weight":1}]}`,
			wantParts: []string{`rule "age_ok"`, `field "id"`, "duplicate id"},
		},
		{
			name:      "empty field",
			input:     `{"version":1,"rules":[{"id":"age_ok","field":"","op":"gte","value":18,"weight":1}]}`,
			wantParts: []string{`rule "age_ok"`, `field "field"`, "must not be empty"},
		},
		{
			name:      "missing version",
			input:     `{"rules":[]}`,
			wantParts: []string{`ruleset field "version"`, "is required"},
		},
		{
			name:      "unsupported version",
			input:     `{"version":2,"rules":[]}`,
			wantParts: []string{`ruleset field "version"`, "unsupported version 2"},
		},
		{
			name:      "negative weight",
			input:     `{"version":1,"rules":[{"id":"age_ok","field":"age","op":"gte","value":18,"weight":-0.25}]}`,
			wantParts: []string{`rule "age_ok"`, `field "weight"`, "must not be negative"},
		},
		{
			name:      "unsupported value type",
			input:     `{"version":1,"rules":[{"id":"age_ok","field":"age","op":"gte","value":{"minimum":18},"weight":1}]}`,
			wantParts: []string{`rule "age_ok"`, `field "value"`, "must be a number, string, or bool"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Decode([]byte(tt.input))
			if err == nil {
				t.Fatal("Decode() error = nil, want an error")
			}
			for _, part := range tt.wantParts {
				if !strings.Contains(err.Error(), part) {
					t.Errorf("Decode() error = %q, want it to contain %q", err, part)
				}
			}
		})
	}
}

func TestRulesetJSONRoundTripPreservesNumbers(t *testing.T) {
	t.Parallel()

	const preciseNumber = "12345678901234567890.12345678901234567890"
	input := []byte(`{"version":1,"rules":[{"id":"precise","field":"amount","op":"eq","value":` + preciseNumber + `,"weight":1}]}`)

	first, err := Decode(input)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if !strings.Contains(string(encoded), preciseNumber) {
		t.Fatalf("json.Marshal() = %s, want exact number %s", encoded, preciseNumber)
	}

	second, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode(round trip) error = %v", err)
	}
	if !reflect.DeepEqual(second, first) {
		t.Fatalf("round trip = %#v, want %#v", second, first)
	}
}
