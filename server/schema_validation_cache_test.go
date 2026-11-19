package server

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSchemaValidationCacheLimit exercises both validators and option orders.
func TestSchemaValidationCacheLimit(t *testing.T) {
	limit := WithSchemaValidationCacheLimit(2)
	tests := []struct {
		name string
		opts []ServerOption
	}{
		{name: "limit first", opts: []ServerOption{limit, WithInputSchemaValidation(), WithOutputSchemaValidation()}},
		{name: "limit last", opts: []ServerOption{WithInputSchemaValidation(), WithOutputSchemaValidation(), limit}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewMCPServer("test", "1.0", tt.opts...)
			input := s.inputValidator.compiledSchemaCache
			output := s.outputValidator.compiledSchemaCache
			_, err := input.lookupOrCompile("input-only", []byte(`{"type":"object"}`))
			require.NoError(t, err)
			require.NotContains(t, output.cached, "input-only")
			input.invalidate("input-only")
			for _, cache := range []*compiledSchemaCache{s.inputValidator.compiledSchemaCache, s.outputValidator.compiledSchemaCache} {
				const first = `{"type":"object","required":["one"]}`
				const second = `{"type":"object","required":["two"]}`
				one, err := cache.lookupOrCompile("same", []byte(first))
				require.NoError(t, err)
				two, err := cache.lookupOrCompile("same", []byte(second))
				require.NoError(t, err)
				_, err = cache.lookupOrCompile("malformed", []byte(`{`))
				require.Error(t, err)
				require.Len(t, cache.cached["same"], 1, "failed compilations count toward the limit")
				cached, err := cache.lookupOrCompile("same", []byte(second))
				require.NoError(t, err)
				require.Same(t, two, cached)
				require.Contains(t, cache.cached["malformed"], schemaDigest([]byte(`{`)))

				recompiled, err := cache.lookupOrCompile("same", []byte(first))
				require.NoError(t, err)
				require.NotSame(t, one, recompiled)
				require.NoError(t, recompiled.Validate(map[string]any{"one": true}))
				require.Error(t, recompiled.Validate(map[string]any{"two": true}))
				require.NotContains(t, cache.cached["same"], schemaDigest([]byte(second)), "a hit does not change insertion order")
				require.Contains(t, cache.cached, "malformed")
			}
		})
	}
}

// TestSchemaValidationCacheLimitAfterPrewarming covers options that make calls during construction.
func TestSchemaValidationCacheLimitAfterPrewarming(t *testing.T) {
	warm := func(s *MCPServer) {
		s.AddTool(fakeListTool(), okHandler)
		requireToolSuccess(t, callTool(t, s, "kubernetes_list", map[string]any{"resourceType": "pods"}))
	}
	s := NewMCPServer("test", "1.0", WithInputSchemaValidation(), warm, WithSchemaValidationCacheLimit(1))
	warm(s)
	_, err := s.inputValidator.lookupOrCompile("other", []byte(`{"type":"object"}`))
	require.NoError(t, err)
	require.Len(t, s.inputValidator.cached, 1)
	require.NotContains(t, s.inputValidator.cached, "kubernetes_list")
	s.DeleteTools("other")
	require.Empty(t, s.inputValidator.cached)
}

// TestSchemaValidationCacheLimitInvalidation keeps eviction bookkeeping in sync with deletion and reset.
func TestSchemaValidationCacheLimitInvalidation(t *testing.T) {
	s := NewMCPServer("test", "1.0", WithInputSchemaValidation(), WithSchemaValidationCacheLimit(2))
	cache := s.inputValidator.compiledSchemaCache
	compile := func(name string) {
		t.Helper()
		_, err := cache.lookupOrCompile(name, []byte(`{"type":"object"}`))
		require.NoError(t, err)
	}
	compile("first")
	compile("second")
	cache.invalidate("first", "first", "missing")
	compile("third")
	require.Contains(t, cache.cached, "second")
	require.Contains(t, cache.cached, "third")
	cache.invalidateAll()
	compile("fourth")
	compile("fifth")
	require.Len(t, cache.cached, 2)
	compile("sixth")
	require.NotContains(t, cache.cached, "fourth")
	require.Len(t, cache.cached, 2)
}
