package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type schemaCacheTestInput struct {
	Name string `json:"name" jsonschema:"Person's name"`
	Age  int    `json:"age" jsonschema:"Person's age"`
}

type schemaCacheTestOutput struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func TestSchemaCache_WarmAndGet(t *testing.T) {
	c := NewSchemaCache()
	require.Equal(t, 0, c.Len())

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
	}
	c.Warm("Foo", schema)

	assert.True(t, c.Has("Foo"))
	assert.False(t, c.Has("Bar"))
	assert.Equal(t, 1, c.Len())
	assert.Equal(t, []string{"Foo"}, c.Keys())

	got, ok := c.Get("Foo")
	require.True(t, ok)
	assert.Equal(t, "object", got["type"])

	// Mutating the returned map must not affect the cached value.
	got["type"] = "string"
	again, _ := c.Get("Foo")
	assert.Equal(t, "object", again["type"])

	// Warming with nil removes the entry.
	c.Warm("Foo", nil)
	assert.False(t, c.Has("Foo"))
	_, ok = c.Get("Foo")
	assert.False(t, ok)
}

func TestSchemaCache_WarmRaw_CopiesInput(t *testing.T) {
	c := NewSchemaCache()
	src := json.RawMessage(`{"type":"object"}`)
	c.WarmRaw("Foo", src)

	// Mutating the source slice must not affect the cached entry.
	for i := range src {
		src[i] = 'x'
	}

	raw, ok := c.GetRaw("Foo")
	require.True(t, ok)
	assert.JSONEq(t, `{"type":"object"}`, string(raw))
}

func TestSchemaCache_NilSafe(t *testing.T) {
	var c *SchemaCache
	assert.False(t, c.Has("x"))
	assert.Equal(t, 0, c.Len())
	assert.Nil(t, c.Keys())

	_, ok := c.Get("x")
	assert.False(t, ok)
	_, ok = c.GetRaw("x")
	assert.False(t, ok)

	// Warm/WarmRaw must not panic on a nil cache.
	c.Warm("x", map[string]any{"type": "string"})
	c.WarmRaw("x", json.RawMessage(`{"type":"string"}`))

	data, err := c.MarshalJSON()
	require.NoError(t, err)
	assert.Equal(t, "{}", string(data))

	require.Error(t, c.UnmarshalJSON([]byte("{}")))
	require.Error(t, c.Save("/tmp/should-not-be-written.json"))
}

func TestSchemaCache_MarshalDeterministic(t *testing.T) {
	c := NewSchemaCache()
	c.WarmRaw("B", json.RawMessage(`{"type":"object"}`))
	c.WarmRaw("A", json.RawMessage(`{"type":"string"}`))
	c.WarmRaw("C", json.RawMessage(`{"type":"number"}`))

	data, err := c.MarshalJSON()
	require.NoError(t, err)

	// Verify keys appear in sorted order.
	idxA := strings.Index(string(data), `"A"`)
	idxB := strings.Index(string(data), `"B"`)
	idxC := strings.Index(string(data), `"C"`)
	require.NotEqual(t, -1, idxA)
	require.NotEqual(t, -1, idxB)
	require.NotEqual(t, -1, idxC)
	assert.Less(t, idxA, idxB)
	assert.Less(t, idxB, idxC)

	// Round-trip should be stable.
	again, err := c.MarshalJSON()
	require.NoError(t, err)
	assert.Equal(t, string(data), string(again))
}

func TestSchemaCache_RoundTripJSON(t *testing.T) {
	c := NewSchemaCache()
	c.Warm("Input", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
		"required": []any{"name"},
	})
	c.Warm("Output", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ok": map[string]any{"type": "boolean"},
		},
	})

	data, err := json.Marshal(c)
	require.NoError(t, err)

	loaded := NewSchemaCache()
	require.NoError(t, json.Unmarshal(data, loaded))
	assert.Equal(t, c.Len(), loaded.Len())
	assert.Equal(t, c.Keys(), loaded.Keys())

	for _, key := range c.Keys() {
		want, _ := c.GetRaw(key)
		got, _ := loaded.GetRaw(key)
		assert.JSONEq(t, string(want), string(got), "round-trip mismatch for %q", key)
	}
}

func TestSchemaCache_UnmarshalReplaces(t *testing.T) {
	c := NewSchemaCache()
	c.WarmRaw("Stale", json.RawMessage(`{"type":"object"}`))
	require.NoError(t, c.UnmarshalJSON([]byte(`{"Fresh":{"type":"string"}}`)))
	assert.False(t, c.Has("Stale"))
	assert.True(t, c.Has("Fresh"))
}

func TestSchemaCache_UnmarshalInvalid(t *testing.T) {
	c := NewSchemaCache()
	require.Error(t, c.UnmarshalJSON([]byte("not json")))
}

func TestSchemaCache_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "schemas.json")

	c := NewSchemaCache()
	require.NoError(t, WarmFor[schemaCacheTestInput](c))
	require.NoError(t, WarmFor[schemaCacheTestOutput](c))
	require.NoError(t, c.Save(path))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(0))

	loaded, err := LoadSchemaCache(path)
	require.NoError(t, err)
	assert.Equal(t, c.Keys(), loaded.Keys())

	for _, key := range c.Keys() {
		want, _ := c.GetRaw(key)
		got, _ := loaded.GetRaw(key)
		assert.JSONEq(t, string(want), string(got))
	}
}

func TestLoadSchemaCache_Missing(t *testing.T) {
	_, err := LoadSchemaCache(filepath.Join(t.TempDir(), "missing.json"))
	require.Error(t, err)
}

func TestSchemaFor_PopulatesCache(t *testing.T) {
	schema := SchemaFor[schemaCacheTestInput]()
	require.NotNil(t, schema)
	assert.Equal(t, "object", schema["type"])

	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, props, "name")
	assert.Contains(t, props, "age")
}

func TestSchemaForRaw_Error(t *testing.T) {
	// Sanity: SchemaForRaw on a well-formed type must succeed and produce
	// non-empty JSON.
	raw, err := SchemaForRaw[schemaCacheTestInput]()
	require.NoError(t, err)
	assert.NotEmpty(t, raw)
}

func TestTypeKey_Stable(t *testing.T) {
	// TypeKey is the package-qualified type name; verify it includes both
	// the package and type identifier.
	k := TypeKey[schemaCacheTestInput]()
	assert.Contains(t, k, "schemaCacheTestInput")
	// Calling twice yields the same key.
	assert.Equal(t, k, TypeKey[schemaCacheTestInput]())
}

func TestWithCachedInputSchema_HitAndMiss(t *testing.T) {
	cache := NewSchemaCache()

	// Cold call: cache miss, schema is generated and stored.
	tool := NewTool("t1", WithCachedInputSchema[schemaCacheTestInput](cache))
	assert.NotEmpty(t, tool.RawInputSchema)
	assert.True(t, cache.Has(TypeKey[schemaCacheTestInput]()))

	// Warm call: cache hit, schema served from cache.
	cached, _ := cache.GetRaw(TypeKey[schemaCacheTestInput]())
	tool2 := NewTool("t2", WithCachedInputSchema[schemaCacheTestInput](cache))
	assert.JSONEq(t, string(cached), string(tool2.RawInputSchema))

	// Marshalled tool exposes the schema under "inputSchema".
	data, err := json.Marshal(tool2)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(data, &out))
	props, ok := out["inputSchema"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "object", props["type"])
}

func TestWithCachedInputSchemaKey_UsesProvidedKey(t *testing.T) {
	cache := NewSchemaCache()
	const key = "stable.input.v1"

	tool := NewTool("t", WithCachedInputSchemaKey[schemaCacheTestInput](cache, key))
	assert.NotEmpty(t, tool.RawInputSchema)
	assert.True(t, cache.Has(key))
	assert.False(t, cache.Has(TypeKey[schemaCacheTestInput]()))
}

func TestWithCachedInputSchema_PrecomputedCacheAvoidsReflection(t *testing.T) {
	cache := NewSchemaCache()
	const key = "stable.input.v1"
	cache.WarmRaw(key, json.RawMessage(`{"type":"object","properties":{"foo":{"type":"string"}}}`))

	tool := NewTool("t", WithCachedInputSchemaKey[schemaCacheTestInput](cache, key))
	assert.JSONEq(t,
		`{"type":"object","properties":{"foo":{"type":"string"}}}`,
		string(tool.RawInputSchema),
	)
}

func TestWithCachedOutputSchema_HitAndMiss(t *testing.T) {
	cache := NewSchemaCache()

	tool := NewTool("t1", WithCachedOutputSchema[schemaCacheTestOutput](cache))
	assert.Equal(t, "object", tool.OutputSchema.Type)
	assert.NotEmpty(t, tool.OutputSchema.Properties)
	assert.True(t, cache.Has(TypeKey[schemaCacheTestOutput]()))

	// Marshalled output should have type=object.
	data, err := json.Marshal(tool)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(data, &out))
	outSchema, ok := out["outputSchema"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "object", outSchema["type"])
}

func TestWithCachedSchemas_NilCacheFallsBackToReflection(t *testing.T) {
	tool := NewTool("t",
		WithCachedInputSchema[schemaCacheTestInput](nil),
		WithCachedOutputSchema[schemaCacheTestOutput](nil),
	)
	assert.NotEmpty(t, tool.RawInputSchema)
	assert.Equal(t, "object", tool.OutputSchema.Type)
}

func TestWarmFor_ExplicitKey(t *testing.T) {
	cache := NewSchemaCache()
	require.NoError(t, WarmFor[schemaCacheTestInput](cache, "custom"))
	assert.True(t, cache.Has("custom"))
	assert.False(t, cache.Has(TypeKey[schemaCacheTestInput]()))
}

func TestWarmFor_NilCacheNoOp(t *testing.T) {
	require.NoError(t, WarmFor[schemaCacheTestInput](nil))
}

func TestWithCachedInputSchemaKey_EmptyKeyBypassesCache(t *testing.T) {
	cache := NewSchemaCache()

	// Two distinct types both called with key="" must not share or pollute
	// any cache entry. Empty key disables the cache entirely for the call.
	tool1 := NewTool("t1", WithCachedInputSchemaKey[schemaCacheTestInput](cache, ""))
	tool2 := NewTool("t2", WithCachedInputSchemaKey[schemaCacheTestOutput](cache, ""))

	assert.NotEmpty(t, tool1.RawInputSchema)
	assert.NotEmpty(t, tool2.RawInputSchema)

	// Each tool must get its own (different) freshly reflected schema, not a
	// shared cached entry that would cause type-2 to receive type-1's schema.
	assert.NotEqual(t, string(tool1.RawInputSchema), string(tool2.RawInputSchema))

	// Cache must remain empty: nothing was read from it and nothing was
	// written to it under the empty key.
	assert.Equal(t, 0, cache.Len())
	assert.False(t, cache.Has(""))
}

func TestWithCachedOutputSchemaKey_EmptyKeyBypassesCache(t *testing.T) {
	cache := NewSchemaCache()

	tool1 := NewTool("t1", WithCachedOutputSchemaKey[schemaCacheTestInput](cache, ""))
	tool2 := NewTool("t2", WithCachedOutputSchemaKey[schemaCacheTestOutput](cache, ""))

	assert.Equal(t, "object", tool1.OutputSchema.Type)
	assert.Equal(t, "object", tool2.OutputSchema.Type)
	assert.NotEmpty(t, tool1.OutputSchema.Properties)
	assert.NotEmpty(t, tool2.OutputSchema.Properties)

	// Distinct types must produce distinct property sets even when called
	// with the same empty key.
	assert.NotEqual(t, tool1.OutputSchema.Properties, tool2.OutputSchema.Properties)

	assert.Equal(t, 0, cache.Len())
	assert.False(t, cache.Has(""))
}

func TestSchemaCache_ConcurrentAccess(t *testing.T) {
	cache := NewSchemaCache()
	var wg sync.WaitGroup
	const workers = 16
	wg.Add(workers)
	for i := range workers {
		go func(i int) {
			defer wg.Done()
			cache.WarmRaw("k"+string(rune('0'+(i%10))), json.RawMessage(`{"type":"object"}`))
			_, _ = cache.GetRaw("k0")
			_ = cache.Has("k1")
			_ = cache.Len()
			_ = cache.Keys()
			_, _ = cache.MarshalJSON()
		}(i)
	}
	wg.Wait()
	assert.LessOrEqual(t, cache.Len(), 10)
}
