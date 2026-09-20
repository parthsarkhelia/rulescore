# rulescore

A small Go library for evaluating weighted scoring rules declared in JSON.

You write a ruleset as JSON — a list of independent comparisons, each with a
weight. `rulescore` decodes and validates it, evaluates it against a record,
and returns a score in `[0, 1]` plus a per-rule breakdown explaining how that
score was reached. No DSL, no reflection, no dependencies outside the standard
library.

```
go get github.com/parthsarkhelia/rulescore
```

Requires Go 1.25 or later.

## The ruleset

A ruleset is a version and a list of rules:

```json
{
  "version": 1,
  "rules": [
    {"id": "adult",    "field": "age",      "op": "gte", "value": 18,   "weight": 2},
    {"id": "verified", "field": "verified", "op": "eq",  "value": true, "weight": 2},
    {"id": "eu",       "field": "region",   "op": "eq",  "value": "eu", "weight": 1}
  ]
}
```

`version` is required and must be `1`. For each rule present, `Decode` rejects
an empty or duplicate `id`, an empty `field`, an unknown `op`, a `value` that
is not a number, string, or bool, and a negative `weight`, naming the rule and
field at fault. It also rejects trailing content after the ruleset object.

`Decode` is not schema-strict, and the gaps are worth knowing:

- **Unknown keys are ignored.** `DisallowUnknownFields` is not set, so a
  misspelled key is silently dropped rather than reported.
- **An omitted `weight` decodes as `0`.** Zero is a valid weight, so a rule
  that misspells `weight` decodes successfully and then contributes nothing to
  any score.
- **An omitted `rules` key decodes as an empty ruleset**, which evaluates every
  record to a score of `0`.

So `Decode` will tell you that a rule you wrote is wrong; it will not tell you
that a key you meant to write is missing.

## Quickstart

This is the body of the `Example` function in
[`example_test.go`](example_test.go), so CI verifies its output on every push to `main` and on every pull request.

```go
rulesetJSON := []byte(`{
  "version": 1,
  "rules": [
    {"id": "adult",    "field": "age",      "op": "gte", "value": 18,   "weight": 2},
    {"id": "verified", "field": "verified", "op": "eq",  "value": true, "weight": 2},
    {"id": "eu",       "field": "region",   "op": "eq",  "value": "eu", "weight": 1}
  ]
}`)

ruleset, err := rulescore.Decode(rulesetJSON)
if err != nil {
	fmt.Println("decode:", err)
	return
}

record := map[string]any{"age": 31.0, "verified": true}

result := ruleset.Evaluate(record)
fmt.Printf("score: %.2f\n", result.Score)
for _, rule := range result.Rules {
	switch {
	case rule.Err != nil:
		fmt.Printf("%-8s error   %v\n", rule.ID, rule.Err)
	case rule.Matched:
		fmt.Printf("%-8s matched weight %g\n", rule.ID, rule.Weight)
	default:
		fmt.Printf("%-8s no match\n", rule.ID)
	}
}
```

```
score: 0.80
adult    matched weight 2
verified matched weight 2
eu       error   field "region": field missing from record
```

The record is the shape `json.Unmarshal` produces for a JSON object. Numbers
may arrive as `float64` (plain `Unmarshal`) or `json.Number` (`Unmarshal` with
`UseNumber`); both are accepted and both score identically. Other Go numeric
types — `int`, `float32` — are not recognised as numbers.

## Operators

Six, and only these six:

| Op    | Meaning               | Applies to             |
|-------|-----------------------|------------------------|
| `eq`  | equal                 | numbers, strings, bools |
| `ne`  | not equal             | numbers, strings, bools |
| `gt`  | greater than          | numbers, strings        |
| `gte` | greater than or equal | numbers, strings        |
| `lt`  | less than             | numbers, strings        |
| `lte` | less than or equal    | numbers, strings        |

Strings are ordered lexicographically by byte, which orders zero-padded and
ISO-8601 values correctly. Bools have no ordering, so `gt`/`gte`/`lt`/`lte` on
a bool is an error.

Numbers are compared exactly, as arbitrary-precision rationals, never through
`float64`. A rule value of `9007199254740993` is therefore genuinely greater
than a record value of `9007199254740992`. A `float64` record value is compared
as the decimal literal it was parsed from, so a record's `0.1` equals a rule's
`0.1`. Precision a `float64` never carried cannot be recovered: decode records
with `UseNumber` if you need integers beyond 2^53.

## What counts as an error

`Evaluate` returns no error value. Every failure belongs to one rule and is
recorded in that rule's `RuleResult.Err`, so a single bad rule neither panics
nor discards the rest of the score.

| Situation | Result |
|-----------|--------|
| Comparison performed, did not hold | `Matched: false`, `Err: nil` |
| Field absent from the record | `Err: ErrFieldMissing` |
| Record and rule values are different kinds | `Err: ErrTypeMismatch` |
| Ordering operator applied to a bool | `Err: ErrNotOrdered` |
| Record number is NaN or ±Inf | `Err` set, no sentinel |
| Rule weight is negative, NaN, or ±Inf | `Err` set, no sentinel |

Two choices here are deliberate and worth knowing before you write rules:

- **A missing field is an error, not a non-match.** `ne` against a missing
  field reports `ErrFieldMissing` rather than reporting `true`. An absent field
  is a statement about the record, not about the value.
- **A type mismatch is an error for every operator, `eq` and `ne` included.**
  Cross-kind values are never equal, but answering "not equal" would hide a
  ruleset or record authoring bug behind a plausible-looking score.

The three sentinels above are wrapped with context, so test them with
`errors.Is`, not `==`. The last two rows carry no sentinel at all: a NaN record
number reports `field "score": record value NaN is not a finite number`, and
`errors.Is` against `ErrNotOrdered` and `ErrTypeMismatch` is false for it. Match
those cases by checking `Err != nil`, not by sentinel.

A rule that could not be evaluated is always `Matched: false`. If your caller
must not absorb bad records silently, reject any `Result` containing a non-nil
`Err`.

## What the score means

`Score` is the sum of the weights of the matched rules divided by the sum of
the weights of every rule whose weight is usable — matched, unmatched, and
unevaluable alike.

The denominator depends only on the ruleset, so the same ruleset scores every
record on the same scale, and a record missing half its fields scores lower
rather than being quietly rescaled against the rules it did answer. Weights are
summed as exact rationals, so the total cannot overflow to ±Inf. If the total
weight is zero, `Score` is `0`, never NaN. `Score` is always in `[0, 1]`.

The one exception: a weight that is not a finite, non-negative number is not a
share of any total, so that rule is left out of **both** sums, not just the
numerator. A ruleset of one matched `weight: 1` rule and one unmatched
`weight: -1` rule scores `1.00`, not `0.50`. `Decode` rejects negative weights
and JSON cannot express NaN or ±Inf, so this only reaches `Evaluate` from a
hand-built `Ruleset`. It is shown in `ExampleRuleset_Evaluate_unusableWeight`.

## Writing rulesets back out

`Encode` is the inverse of `Decode`. It validates the ruleset and returns
compact, byte-stable JSON; a successful `Encode` fed back to `Decode` yields a
`Ruleset` equal field-for-field to the original, including rule order, each
value's dynamic type, and the exact decimal text of a `json.Number`.

Hand-built rulesets are held to the same rules as decoded ones: `Rule.Value`
must be a `string`, `bool`, or valid `json.Number`, and is rejected rather than
coerced if it is anything else.

## Limitations

These are real and unlikely to change soon. If any of them is a problem for
you, this is not the library you want.

- **`field` is a flat key.** Nested paths such as `user.address.city` are not
  supported. The field name is looked up directly in the record map.
- **Six comparison operators only.** There is no `in`, `contains`, `matches`,
  or `exists`.
- **Rules are independent and additively weighted.** There is no AND/OR
  grouping and no nesting. You cannot express "A and either B or C".
- **No custom functions.** Comparisons are the six operators against a literal
  value; you cannot call your own code from a rule.
- **The API is not stable yet.** Expect breaking changes before v1.

There are no benchmarks in this repository, so it makes no performance claims.

## Documentation

`go doc -all github.com/parthsarkhelia/rulescore`, or
[pkg.go.dev](https://pkg.go.dev/github.com/parthsarkhelia/rulescore). The doc
comments on `Evaluate` and `Encode` are the authoritative statement of the
semantics summarised above.

## License

MIT — see [LICENSE](LICENSE).
