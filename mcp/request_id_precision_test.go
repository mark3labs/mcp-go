package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestIDIntegerPrecision(t *testing.T) {
	for _, raw := range []string{"1", "9007199254740991", "9007199254740992", "9007199254740993", "-9007199254740993", "9223372036854775807", "-9223372036854775808", `"9007199254740993"`, "null", "1.5"} {
		t.Run(raw, func(t *testing.T) {
			var id RequestId
			require.NoError(t, json.Unmarshal([]byte(raw), &id))
			encoded, err := json.Marshal(id)
			require.NoError(t, err)
			require.Equal(t, raw, string(encoded))
		})
	}
	var left, right RequestId
	require.NoError(t, json.Unmarshal([]byte("9007199254740992"), &left))
	require.NoError(t, json.Unmarshal([]byte("9007199254740993"), &right))
	require.NotEqual(t, left.String(), right.String())
}

func TestRequestIDExtendedIntegerIdentity(t *testing.T) {
	tests := []struct {
		name  string
		forms []string
		value any
	}{
		{"int64 exponent", []string{"9007199254740993", "9007199254740993e0", "90071992547409930e-1", "9007199254740993.0"}, int64(9007199254740993)},
		{"uint64", []string{"18446744073709551615", "184467440737095516150e-1", "18446744073709551615.000"}, uint64(18446744073709551615)},
		{"compact huge exponent", []string{"1e1000000000000000000", "10e999999999999999999"}, json.Number("1e1000000000000000000")},
		{"zero", []string{"0", "-0", "0e1000000000000000000"}, int64(0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expected := NewRequestId(tt.value).String()
			for _, raw := range tt.forms {
				var id RequestId
				require.NoError(t, json.Unmarshal([]byte(raw), &id))
				require.Equal(t, expected, id.String(), raw)
				encoded, err := json.Marshal(id)
				require.NoError(t, err)
				var decoded RequestId
				require.NoError(t, json.Unmarshal(encoded, &decoded))
				require.Equal(t, expected, decoded.String(), raw)
			}
		})
	}
}

func TestNotificationParamsRetainsOtherFields(t *testing.T) {
	var params NotificationParams
	require.NoError(t, json.Unmarshal([]byte(`{"requestId":9007199254740993e0,"count":2,"_meta":{"n":3}}`), &params))
	require.Equal(t, int64(9007199254740993), params.AdditionalFields["requestId"])
	require.Equal(t, float64(2), params.AdditionalFields["count"])
	require.Equal(t, float64(3), params.Meta["n"])
	require.NoError(t, json.Unmarshal([]byte(`{"requestId":{"custom":true}}`), &params))
	require.Equal(t, map[string]any{"custom": true}, params.AdditionalFields["requestId"])
}
