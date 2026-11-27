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

				// Preserve the input format for get/cancel; list remains legacy.
				// The int64 value must survive the round trip exactly.
				if kind == "list" || wire.name == "legacy-input" {
					// tasks/list serves legacy 2025-11-25 clients (ttl/pollInterval)
					require.Contains(t, string(encoded), `"ttl":`+bigInt)
					require.Contains(t, string(encoded), `"pollInterval":`+bigInt)
				} else {
					// Modern get/cancel responses retain SEP-2663 timing names.
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

func TestTaskResponsesPreserveTimingFormat(t *testing.T) {
	tests := []struct {
		name   string
		wire   string
		legacy bool
	}{
		{name: "legacy timing", wire: `{"taskId":"t","status":"cancelled","ttl":60000,"pollInterval":1000}`, legacy: true},
		{name: "legacy unlimited", wire: `{"taskId":"t","status":"cancelled","ttl":null}`, legacy: true},
		{name: "modern timing", wire: `{"resultType":"complete","taskId":"t","status":"completed","ttlMs":60000,"pollIntervalMs":1000}`},
		{name: "modern unlimited", wire: `{"resultType":"complete","taskId":"t","status":"completed","ttlMs":null}`},
	}
	for _, tt := range tests {
		for _, kind := range []string{"get", "cancel"} {
			for _, decoder := range []string{"parser", "json"} {
				t.Run(tt.name+"/"+kind+"/"+decoder, func(t *testing.T) {
					raw := json.RawMessage(tt.wire)
					var result any
					var legacy bool
					if kind == "get" {
						got := &GetTaskResult{}
						if decoder == "parser" {
							var err error
							got, err = ParseGetTaskResult(&raw)
							require.NoError(t, err)
						} else {
							require.NoError(t, json.Unmarshal(raw, got))
						}
						result, legacy = got, got.Legacy
					} else {
						got := &CancelTaskResult{}
						if decoder == "parser" {
							var err error
							got, err = ParseCancelTaskResult(&raw)
							require.NoError(t, err)
						} else {
							require.NoError(t, json.Unmarshal(raw, got))
						}
						result, legacy = got, got.Legacy
					}
					require.Equal(t, tt.legacy, legacy)
					encoded, err := json.Marshal(result)
					require.NoError(t, err)
					var fields map[string]json.RawMessage
					require.NoError(t, json.Unmarshal(encoded, &fields))
					if tt.legacy {
						require.Contains(t, fields, "ttl")
						require.NotContains(t, fields, "ttlMs")
						require.NotContains(t, fields, "pollIntervalMs")
					} else {
						require.Contains(t, fields, "ttlMs")
						require.NotContains(t, fields, "ttl")
						require.NotContains(t, fields, "pollInterval")
					}
					var original map[string]json.RawMessage
					require.NoError(t, json.Unmarshal(raw, &original))
					for key, value := range original {
						require.JSONEq(t, string(value), string(fields[key]), "field %s", key)
					}
				})
			}
		}
	}
}

func TestTaskResponseUnmarshalResetsLegacyFormat(t *testing.T) {
	legacy := []byte(`{"taskId":"t","status":"cancelled","ttl":60000,"pollInterval":1000}`)
	var get GetTaskResult
	require.NoError(t, json.Unmarshal(legacy, &get))
	require.True(t, get.Legacy)
	require.NoError(t, json.Unmarshal([]byte(`{"taskId":"t","status":"working","ttlMs":null}`), &get))
	require.False(t, get.Legacy)
	require.Nil(t, get.TTL)
	require.Nil(t, get.PollInterval)

	var cancel CancelTaskResult
	require.NoError(t, json.Unmarshal(legacy, &cancel))
	require.True(t, cancel.Legacy)
	ack := json.RawMessage(`{"resultType":"complete"}`)
	require.NoError(t, json.Unmarshal(ack, &cancel))
	require.False(t, cancel.Legacy)
	require.Empty(t, cancel.TaskId)
	encoded, err := json.Marshal(cancel)
	require.NoError(t, err)
	require.JSONEq(t, string(ack), string(encoded))
	parsed, err := ParseCancelTaskResult(&ack)
	require.NoError(t, err)
	require.False(t, parsed.Legacy)
	encoded, err = json.Marshal(parsed)
	require.NoError(t, err)
	require.JSONEq(t, string(ack), string(encoded))
}
