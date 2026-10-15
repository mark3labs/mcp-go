package mcp

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	first "github.com/mark3labs/mcp-go/mcp/testdata/schema_cache/first"
	second "github.com/mark3labs/mcp-go/mcp/testdata/schema_cache/second"
)

type schemaCacheEnvelope[T any] struct {
	Value T `json:"value"`
}

func checkCachedSchemaIdentity[A, B any](t *testing.T) {
	t.Helper()
	assert.NotEqual(t, TypeKey[A](), TypeKey[B]())

	for _, warm := range []bool{false, true} {
		cache := NewSchemaCache()
		if warm {
			require.NoError(t, WarmFor[A](cache))
			require.NoError(t, WarmFor[B](cache))
			path := filepath.Join(t.TempDir(), "schemas.json")
			require.NoError(t, cache.Save(path))
			var err error
			cache, err = LoadSchemaCache(path)
			require.NoError(t, err)
		}

		wantA, err := json.Marshal(NewTool("a", WithInputSchema[A](), WithOutputSchema[A]()))
		require.NoError(t, err)
		wantB, err := json.Marshal(NewTool("b", WithInputSchema[B](), WithOutputSchema[B]()))
		require.NoError(t, err)
		gotA, err := json.Marshal(NewTool("a", WithCachedInputSchema[A](cache), WithCachedOutputSchema[A](cache)))
		require.NoError(t, err)
		gotB, err := json.Marshal(NewTool("b", WithCachedInputSchema[B](cache), WithCachedOutputSchema[B](cache)))
		require.NoError(t, err)
		assert.JSONEq(t, string(wantA), string(gotA), "warm=%v", warm)
		assert.JSONEq(t, string(wantB), string(gotB), "warm=%v", warm)
		assert.Equal(t, 2, cache.Len(), "warm=%v", warm)
	}
}

func TestCachedSchemas_DistinguishPackagesWithTheSameName(t *testing.T) {
	t.Run("named", checkCachedSchemaIdentity[first.Request, second.Request])
	t.Run("pointer", checkCachedSchemaIdentity[*first.Request, *second.Request])
	t.Run("generic wrapper", checkCachedSchemaIdentity[schemaCacheEnvelope[first.Request], schemaCacheEnvelope[second.Request]])
	t.Run("anonymous struct", checkCachedSchemaIdentity[struct {
		Request first.Request `json:"request"`
	}, struct {
		Request second.Request `json:"request"`
	}])
	t.Run("slice field", checkCachedSchemaIdentity[struct {
		Requests []first.Request `json:"requests"`
	}, struct {
		Requests []second.Request `json:"requests"`
	}])
	t.Run("array field", checkCachedSchemaIdentity[struct {
		Requests [2]first.Request `json:"requests"`
	}, struct {
		Requests [2]second.Request `json:"requests"`
	}])
	t.Run("map field", checkCachedSchemaIdentity[struct {
		Requests map[string]first.Request `json:"requests"`
	}, struct {
		Requests map[string]second.Request `json:"requests"`
	}])
}

func TestCachedSchemas_OldKeysAreRegenerated(t *testing.T) {
	cache := NewSchemaCache()
	oldKey := reflect.TypeFor[second.Request]().String()
	cache.Warm(oldKey, SchemaFor[first.Request]())
	path := filepath.Join(t.TempDir(), "schemas.json")
	require.NoError(t, cache.Save(path))
	loaded, err := LoadSchemaCache(path)
	require.NoError(t, err)

	want, err := json.Marshal(NewTool("t", WithInputSchema[second.Request](), WithOutputSchema[second.Request]()))
	require.NoError(t, err)
	got, err := json.Marshal(NewTool("t", WithCachedInputSchema[second.Request](loaded), WithCachedOutputSchema[second.Request](loaded)))
	require.NoError(t, err)
	assert.JSONEq(t, string(want), string(got))
	assert.True(t, loaded.Has(oldKey))
	assert.True(t, loaded.Has(TypeKey[second.Request]()))
}
