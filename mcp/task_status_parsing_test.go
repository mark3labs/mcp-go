package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTaskStatusParsersPreserveEnvelopeAndIntegerFields(t *testing.T) {
	for _, kind := range []string{"get", "cancel", "list"} {
		t.Run(kind, func(t *testing.T) {
			task := `{"taskId":"task","status":"completed","createdAt":"2026-10-09T00:00:00Z","lastUpdatedAt":"2026-10-09T00:00:00Z","ttl":9007199254740993,"pollInterval":9007199254740993}`
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
			require.Contains(t, string(encoded), `"ttl":9007199254740993`)
			require.Contains(t, string(encoded), `"pollInterval":9007199254740993`)
		})
	}
}
