// Package rulescore evaluates independent, weighted comparisons declared in
// JSON. It returns both a score and a per-rule explanation of how that score
// was reached.
//
// A ruleset has a schema version and an ordered list of rules. Each rule names
// a top-level record field, one of the operators eq, ne, gt, gte, lt, or lte, a
// JSON number, string, or bool to compare against, and a non-negative weight.
// The README works one example through in full: comparing flats to rent, where
// the criteria are the ruleset and each listing is a record.
//
//	{
//	  "version": 1,
//	  "rules": [
//	    {"id":"rent", "field":"rent_pcm", "op":"lte", "value":1500, "weight":4},
//	    {"id":"pets", "field":"pets_allowed", "op":"eq", "value":true, "weight":2}
//	  ]
//	}
//
// Decode parses and validates this JSON strictly: missing required fields,
// unknown fields, duplicate IDs, unsupported operators and values, negative
// weights, and trailing JSON are errors. A typical decode-evaluate-encode flow
// is:
//
//	rs, err := rulescore.Decode(data)
//	if err != nil {
//	    return err
//	}
//
//	result := rs.Evaluate(map[string]any{"rent_pcm": 1400.0, "pets_allowed": true})
//	for _, rule := range result.Rules {
//	    // Inspect rule.Matched and rule.Err.
//	}
//
//	data, err = rulescore.Encode(rs)
//	if err != nil {
//	    return err
//	}
//
// Encode validates hand-built rulesets as well as producing compact,
// byte-stable JSON. Everything successfully encoded can be decoded without
// losing rule order, value type, or json.Number text.
//
// Result.Score is the sum of matched-rule weights divided by the sum of all
// usable rule weights, including weights for rules that did not match or could
// not be evaluated. A zero total produces 0, and the score is always in
// [0, 1]. A non-finite or negative weight in a hand-built ruleset is unusable
// and is excluded from both sums.
//
// Evaluate returns no top-level error. A rule that cannot be evaluated has a
// non-nil RuleResult.Err that wraps exactly one of ErrFieldMissing,
// ErrTypeMismatch, ErrNotOrdered, ErrInvalidRule, or ErrInvalidRecord. Test
// these classifications with errors.Is. A false Matched value with a nil Err
// is an ordinary non-match; a non-nil Err means no comparison was possible.
// Decode and Encode instead return validation errors directly.
//
// Rulescore does not provide nested field paths, logical rule groups, custom
// functions, value coercion, or operators beyond the six comparisons above.
// It does not fetch records or decide whether an application should accept a
// Result containing per-rule errors; callers supply the data and that policy.
package rulescore
