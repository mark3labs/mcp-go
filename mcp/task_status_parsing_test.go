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

// TestTaskParsersRejectMalformedTiming verifies that a wrong-type or
// out-of-range ttlMs/pollIntervalMs surfaces an error instead of being silently
// dropped (a dropped ttlMs would read as "no expiration").
func TestTaskParsersRejectMalformedTiming(t *testing.T) {
	for _, tc := range []struct {
		name string
		task string
	}{
		{"string ttlMs", `{"taskId":"t","status":"working","ttlMs":"nope"}`},
		{"string pollIntervalMs", `{"taskId":"t","status":"working","pollIntervalMs":"nope"}`},
		{"bool ttlMs", `{"taskId":"t","status":"working","ttlMs":true}`},
		{"overflow ttlMs", `{"taskId":"t","status":"working","ttlMs":99999999999999999999}`},
		{"valid ttlMs, bad poll", `{"taskId":"t","status":"working","ttlMs":60000,"pollIntervalMs":"nope"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			get := json.RawMessage(tc.task)
			_, err := ParseGetTaskResult(&get)
			require.Error(t, err, "ParseGetTaskResult")

			cancel := json.RawMessage(tc.task)
			_, err = ParseCancelTaskResult(&cancel)
			require.Error(t, err, "ParseCancelTaskResult")

			list := json.RawMessage(`{"tasks":[` + tc.task + `]}`)
			_, err = ParseListTasksResult(&list)
			require.Error(t, err, "ParseListTasksResult")
		})
	}
}

// TestTaskParserDecodesModernTiming verifies a valid SEP-2663 ttlMs is populated
// through the parser (the Task struct's legacy tags would otherwise skip it).
func TestTaskParserDecodesModernTiming(t *testing.T) {
	raw := json.RawMessage(`{"taskId":"t","status":"working","ttlMs":60000}`)
	result, err := ParseGetTaskResult(&raw)
	require.NoError(t, err)
	require.NotNil(t, result.TTL, "valid ttlMs must be populated")
	require.Equal(t, int64(60000), *result.TTL)
	require.Nil(t, result.PollInterval, "absent pollIntervalMs must stay nil")
}
