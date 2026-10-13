# Validate integer argument conversions before narrowing

## Context
Issue1081 reproduces decimal-looking strings interpreted as octal, malformed arguments becoming zero, and narrow integer overflow wrapping. The existing docs promise defaults for missing arguments only, and delegate coercion to spf13/cast; they do not establish decimal string or invalid-value semantics. Changing these semantics is deliberate, not a claim about an existing MCP wire requirement.

## Decision
Parse string and json.Number values as decimal integers at the destination width. Render native signed/unsigned integers without narrowing before parsing. Validate native float ranges before preserving the existing truncation toward zero. Preserve boolean numeric conversion. Return the caller default for malformed, missing, unsupported or out-of-range input. Apply the same contract to all ten helpers together.

## Alternatives
Keeping base auto-detection silently changes zero-padded tool arguments. Narrowing first loses the original out-of-range value. Adding ten unrelated fixes would fragment one shared conversion contract. Changing helper signatures to return errors would break callers.

## Consequences
Padded decimal strings now mean decimal. Explicit radix strings such as 0x10 and unsupported pointer/container coercions return the default rather than relying on cast's broad coercions. This is an observable compatibility change and must be reviewed as such. Native float truncation and bool conversion remain available; default values are never narrowed accidentally. No tool is executed by these tests.

## Validation
Same new assertions evaluated against frozen fd2a63fb9f5e74bfc98415acb18fa97ac7d3d319 fail for padded numbers, malformed conversions and overflow. Candidate focused tests and full MCP package race tests pass on isolated Go1.27.2/macOS with GOMAXPROCS=2 and -p1. Cases cover every signed/unsigned helper, missing/null/container/non-finite controls, signed bounds, unsigned negative/narrowing and maximum uint64. Logs remain in the private campaign evidence. Full repository/submodule race tests and golangci-lint have not yet run; no hosted or live-client behavior is claimed.
