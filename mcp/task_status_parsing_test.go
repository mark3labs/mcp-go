package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTaskStatusParsersPreserveEnvelopeAndIntegerFields(t *testing.T) {
	// bigInt is 2^53+1, which is not exactly representable as a float64. Parsing
	// must preserve it as an int64, so decoding must not route through float64.
	const bigInt = `9007199254740993`

	for _, wire := range []struct {
		name      string
		ttlField  string
		pollField string
	}{
		// Legacy (ttl/pollInterval): decoded by the struct's json tags.
		// Modern (ttlMs/pollIntervalMs): decoded by jsonToTask.
		{"legacy-input", "ttl", "pollInterval"},
		{"modern-input", "ttlMs", "pollIntervalMs"},
	} {
		for _, kind := range []string{"get", "cancel", "list"} {
			t.Run(wire.name+"/"+kind, func(t *testing.T) {
				task := `{"taskId":"task","status":"completed",` +
					`"createdAt":"2026-10-09T00:00:00Z","lastUpdatedAt":"2026-10-09T00:00:00Z",` +
					`"` + wire.ttlField + `":` + bigInt + `,"` + wire.pollField + `":` + bigInt + `}`
				raw := json.RawMessage(`{"resultType":"data","_meta":{"key":"value"},` + task[1:])
				if kind == "list" {
					raw = json.RawMessage(`{"resultType":"data","_meta":{"key":"value"},"tasks":[` + task + `]}`)
				}

				var result any
				var err error
				switch kind {
				case "get":
					result, err = ParseGetTaskResult(&raw)
				case "cancel":
					result, err = ParseCancelTaskResult(&raw)
				default:
					result, err = ParseListTasksResult(&raw)
				}
				require.NoError(t, err)

				encoded, err := json.Marshal(result)
				require.NoError(t, err)
				var fields map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(encoded, &fields))
				require.JSONEq(t, `"data"`, string(fields["resultType"]))

				// The output wire names depend on the response type, not the
				// input. The int64 value must survive the round trip exactly.
				if kind == "list" {
					// tasks/list serves legacy 2025-11-25 clients (ttl/pollInterval)
					require.Contains(t, string(encoded), `"ttl":`+bigInt)
					require.Contains(t, string(encoded), `"pollInterval":`+bigInt)
				} else {
					// tasks/get and tasks/cancel use SEP-2663 names (ttlMs/pollIntervalMs).
					require.Contains(t, string(encoded), `"ttlMs":`+bigInt)
					require.Contains(t, string(encoded), `"pollIntervalMs":`+bigInt)
				}
			})
		}
	}
}
