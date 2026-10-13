# Validate integer argument conversions before narrowing

## Context
Issue 1081 reproduces decimal-looking strings interpreted as octal, malformed arguments becoming zero, and narrow integer overflow wrapping. The existing docs promise defaults for missing arguments only, and delegate coercion to spf13/cast; they do not establish decimal string or invalid-value semantics. Changing these semantics is deliberate, not a claim about an existing MCP wire requirement.

## Decision
Parse string and json.Number values as decimal integers at the destination width. Render native signed/unsigned integers without narrowing before parsing. Validate native float ranges before preserving the existing truncation toward zero. Preserve boolean numeric conversion. Return the caller default for malformed, missing, unsupported or out-of-range input. Apply the same contract to all ten helpers together.

## Alternatives
Keeping base auto-detection silently changes zero-padded tool arguments. Narrowing first loses the original out-of-range value. Adding ten unrelated fixes would fragment one shared conversion contract. Changing helper signatures to return errors would break callers.

## Consequences
Padded decimal strings now mean decimal. Explicit radix strings such as 0x10 and unsupported pointer/container coercions return the default rather than relying on cast's broad coercions. This is an observable compatibility change and must be reviewed as such. Native float truncation and bool conversion remain available; default values are never narrowed accidentally. No tool is executed by these tests.

## Validation
Same new assertions evaluated against frozen fd2a63fb9f5e74bfc98415acb18fa97ac7d3d319 fail for padded numbers, malformed conversions and overflow. Focused candidate tests and full MCP package race tests pass on isolated Go 1.27.2/macOS with GOMAXPROCS=2 and -p1. Cases cover every signed/unsigned helper, missing/null/container/non-finite controls, signed bounds, unsigned negative/narrowing and maximum uint64. Logs remain in the private campaign evidence. Full repository/submodule race tests and golangci-lint have not yet run; no hosted or live-client behavior is claimed.

## Independent review correction
The first reviewed candidate 4b2e5d4c2ff60aae561636965c768cc1245401c8 was held for whitespace compatibility and negative fractional unsigned input. Strings now retain the existing surrounding-whitespace acceptance before decimal parsing. Unsigned floats reject an originally negative value before truncation; positive fractions still truncate. New exact assertions cover both behaviors; frozen baseline and candidate logs remain separate.

## Frozen broader validation
Corrected frozen b7d724fe1b39e6ceaa51a5aa259765e2a40d6f20 completed `GOMAXPROCS=2 go test -p 1 ./... -race -count=1 -timeout=10m` successfully for the root module. The separate `otel` module race command also passed. Focused transport teardown test passed on both baseline (20.189s) and candidate (30.111s), each with a declared 2m cap. An earlier unfrozen broad run hit its 5m transport-package cap while review correction overlapped; it remains diagnostic history, not exact revision evidence. No cause/flakiness conclusion is inferred from these runs. Golangci-lint and hosted CI remain unverified. This record-only revision changes no implementation or assertions.

## Hosted review correction
Review 4261789616 identified that signed native floats were truncated before the lower-bound check: -128.5 incorrectly became valid -128 for int8. Check the original float against the signed admission interval [-2^(bits-1), 2^(bits-1)) before truncating. In-range fractions still truncate toward zero. Added float32/float64 lower-bound regression and exact-minimum, negative-fraction and positive-fraction controls. Against frozen parent 17e0661fa1f7b0005ca7e7fbcc17a583a8369a82, the two new underflow cases fail; the controls pass. Corrected focused and full MCP race checks pass. Historical full-root/otel evidence above belongs to its named prior revision, not this correction. This correction has not repeated broad transport/platform or hosted CI checks. ADR spacing was repaired without changing historical measurements.
