package mcp

import "testing"

var benchmarkContent Content

func BenchmarkUnmarshalContent(b *testing.B) {
	cases := []struct {
		name string
		data []byte
	}{
		{
			name: "text",
			data: []byte(`{"type":"text","text":"The quick brown fox jumps over the lazy dog."}`),
		},
		{
			name: "tool_use",
			data: []byte(`{"type":"tool_use","id":"call_123","name":"lookup_weather","input":{"city":"London","unit":"celsius","includeForecast":true}}`),
		},
		{
			name: "tool_result",
			data: []byte(`{"type":"tool_result","toolUseId":"call_123","content":[{"type":"text","text":"Sunny, 22C"}],"isError":false}`),
		},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				content, err := UnmarshalContent(tc.data)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkContent = content
			}
		})
	}
}
