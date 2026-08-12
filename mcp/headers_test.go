package mcp

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeDecodeHeaderValue(t *testing.T) {
	tests := []struct {
		name    string
		value   any
		encoded string
	}{
		{"plain ascii is used as-is", "us-east-1", "us-east-1"},
		{"integers are decimal", int64(42), "42"},
		{"booleans are lowercase", true, "true"},
		{"non-ascii is base64 wrapped", "café", "=?base64?Y2Fmw6k=?="},
		{"control characters are base64 wrapped", "a\nb", "=?base64?YQpi?="},
		{"leading whitespace is base64 wrapped", " lead", "=?base64?IGxlYWQ=?="},
		{"trailing whitespace is base64 wrapped", "trail ", "=?base64?dHJhaWwg?="},
		// A plain value that looks like the sentinel must itself be encoded,
		// so it cannot be mistaken for an already-encoded one.
		{"sentinel lookalikes are escaped", "=?base64?notreally?=", "=?base64?PT9iYXNlNjQ/bm90cmVhbGx5Pz0=?="},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, ok := EncodeHeaderValue(tt.value)
			require.True(t, ok)
			assert.Equal(t, tt.encoded, encoded)

			decoded, ok := DecodeHeaderValue(encoded)
			require.True(t, ok)

			expected, ok := primitiveToHeaderString(tt.value)
			require.True(t, ok)
			assert.Equal(t, expected, decoded, "encoding must round-trip")
		})
	}
}

func TestEncodeHeaderValueRejectsNonPrimitives(t *testing.T) {
	for _, value := range []any{1.5, []string{"a"}, map[string]any{}, nil} {
		_, ok := EncodeHeaderValue(value)
		assert.False(t, ok, "%v is not a permitted header value", value)
	}
}

func TestDecodeHeaderValueRejectsBadBase64(t *testing.T) {
	_, ok := DecodeHeaderValue("=?base64?not-valid-base64!?=")
	assert.False(t, ok)
}

func TestExtractHeaderName(t *testing.T) {
	tests := []struct {
		name   string
		method MCPMethod
		params string
		want   string
		ok     bool
	}{
		{"tools/call uses the tool name", MethodToolsCall, `{"name":"search"}`, "search", true},
		{"prompts/get uses the prompt name", MethodPromptsGet, `{"name":"summarize"}`, "summarize", true},
		{"resources/read uses the uri", MethodResourcesRead, `{"uri":"file:///a"}`, "file:///a", true},
		{"other methods carry no name", MethodToolsList, `{}`, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ExtractHeaderName(tt.method, json.RawMessage(tt.params))
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestStandardHeadersOnlyForModernVersions(t *testing.T) {
	params := json.RawMessage(`{"name":"search"}`)

	assert.Nil(t, StandardHeaders(ProtocolVersion20251125, MethodToolsCall, params),
		"earlier revisions did not define these headers")

	headers := StandardHeaders(ProtocolVersion20260728, MethodToolsCall, params)
	assert.Equal(t, string(MethodToolsCall), headers[HeaderMethod])
	assert.Equal(t, "search", headers[HeaderName])
}

func TestValidateStandardHeaders(t *testing.T) {
	callParams := json.RawMessage(`{"name":"search"}`)

	tests := []struct {
		name    string
		headers map[string]string
		version string
		method  MCPMethod
		params  json.RawMessage
		wantErr bool
	}{
		{
			name:    "earlier revisions are not checked",
			headers: nil,
			version: ProtocolVersion20251125,
			method:  MethodToolsCall,
			params:  callParams,
		},
		{
			name:    "matching headers pass",
			headers: map[string]string{HeaderMethod: "tools/call", HeaderName: "search"},
			version: ProtocolVersion20260728,
			method:  MethodToolsCall,
			params:  callParams,
		},
		{
			name:    "a base64 encoded name is decoded before comparison",
			headers: map[string]string{HeaderMethod: "tools/call", HeaderName: "=?base64?c2VhcmNo?="},
			version: ProtocolVersion20260728,
			method:  MethodToolsCall,
			params:  callParams,
		},
		{
			name:    "methods without a name need no name header",
			headers: map[string]string{HeaderMethod: "tools/list"},
			version: ProtocolVersion20260728,
			method:  MethodToolsList,
			params:  json.RawMessage(`{}`),
		},
		{
			name:    "a missing method header is rejected",
			headers: map[string]string{HeaderName: "search"},
			version: ProtocolVersion20260728,
			method:  MethodToolsCall,
			params:  callParams,
			wantErr: true,
		},
		{
			name:    "a method header disagreeing with the body is rejected",
			headers: map[string]string{HeaderMethod: "tools/list", HeaderName: "search"},
			version: ProtocolVersion20260728,
			method:  MethodToolsCall,
			params:  callParams,
			wantErr: true,
		},
		{
			name:    "a missing name header is rejected",
			headers: map[string]string{HeaderMethod: "tools/call"},
			version: ProtocolVersion20260728,
			method:  MethodToolsCall,
			params:  callParams,
			wantErr: true,
		},
		{
			name:    "a name header disagreeing with the body is rejected",
			headers: map[string]string{HeaderMethod: "tools/call", HeaderName: "other"},
			version: ProtocolVersion20260728,
			method:  MethodToolsCall,
			params:  callParams,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := http.Header{}
			for name, value := range tt.headers {
				header.Set(name, value)
			}

			err := ValidateStandardHeaders(header.Get, tt.version, tt.method, tt.params)
			if !tt.wantErr {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.True(t, IsHeaderMismatch(err), "want a header mismatch, got %v", err)
		})
	}
}

// headerTool is a tool whose region parameter travels in an HTTP header, and
// whose nested config.debug parameter does too.
func headerTool() *Tool {
	return &Tool{
		Name: "query",
		RawInputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"sql": { "type": "string" },
				"region": { "type": "string", "x-mcp-header": "Region" },
				"config": {
					"type": "object",
					"properties": {
						"debug": { "type": "boolean", "x-mcp-header": "Debug" }
					}
				}
			}
		}`),
	}
}

func TestExtractParamHeaderBindings(t *testing.T) {
	bindings := ExtractParamHeaderBindings(headerTool())
	require.Len(t, bindings, 2)

	byHeader := map[string][]string{}
	for _, binding := range bindings {
		byHeader[binding.Header] = binding.Path
	}
	assert.Equal(t, []string{"region"}, byHeader["Region"])
	assert.Equal(t, []string{"config", "debug"}, byHeader["Debug"], "annotations nest")
}

func TestGenerateParamHeaders(t *testing.T) {
	params := json.RawMessage(`{
		"name": "query",
		"arguments": {
			"sql": "select 1",
			"region": "us-east-1",
			"config": { "debug": true }
		}
	}`)

	headers := GenerateParamHeaders(headerTool(), params)
	assert.Equal(t, "us-east-1", headers[HeaderParamPrefix+"Region"])
	assert.Equal(t, "true", headers[HeaderParamPrefix+"Debug"])
	assert.NotContains(t, headers, HeaderParamPrefix+"sql", "unannotated parameters stay in the body")
}

func TestGenerateParamHeadersSkipsAbsentArguments(t *testing.T) {
	params := json.RawMessage(`{"name":"query","arguments":{"sql":"select 1"}}`)
	assert.Empty(t, GenerateParamHeaders(headerTool(), params))
}

func TestValidateParamHeaders(t *testing.T) {
	tool := headerTool()
	params := json.RawMessage(`{
		"name": "query",
		"arguments": { "region": "us-east-1", "config": { "debug": true } }
	}`)

	t.Run("mirrored headers pass", func(t *testing.T) {
		header := http.Header{}
		for name, value := range GenerateParamHeaders(tool, params) {
			header.Set(name, value)
		}
		assert.NoError(t, ValidateParamHeaders(header.Get, tool, params))
	})

	t.Run("a missing header is rejected", func(t *testing.T) {
		header := http.Header{}
		header.Set(HeaderParamPrefix+"Debug", "true")
		err := ValidateParamHeaders(header.Get, tool, params)
		require.Error(t, err)
		assert.True(t, IsHeaderMismatch(err))
	})

	t.Run("a header disagreeing with the body is rejected", func(t *testing.T) {
		header := http.Header{}
		header.Set(HeaderParamPrefix+"Region", "eu-west-1")
		header.Set(HeaderParamPrefix+"Debug", "true")
		err := ValidateParamHeaders(header.Get, tool, params)
		require.Error(t, err)
		assert.True(t, IsHeaderMismatch(err))
	})

	t.Run("a header for an absent parameter is rejected", func(t *testing.T) {
		absent := json.RawMessage(`{"name":"query","arguments":{}}`)
		header := http.Header{}
		header.Set(HeaderParamPrefix+"Region", "us-east-1")
		err := ValidateParamHeaders(header.Get, tool, absent)
		require.Error(t, err)
		assert.True(t, IsHeaderMismatch(err))
	})
}

func TestValidateParamHeaderAnnotations(t *testing.T) {
	tests := []struct {
		name    string
		schema  string
		wantErr string
	}{
		{
			name:   "a valid annotation is accepted",
			schema: `{"type":"object","properties":{"a":{"type":"string","x-mcp-header":"A"}}}`,
		},
		{
			name:    "non-primitive types are rejected",
			schema:  `{"type":"object","properties":{"a":{"type":"array","x-mcp-header":"A"}}}`,
			wantErr: "primitive types",
		},
		{
			name:    "empty header names are rejected",
			schema:  `{"type":"object","properties":{"a":{"type":"string","x-mcp-header":""}}}`,
			wantErr: "non-empty string",
		},
		{
			name:    "invalid token characters are rejected",
			schema:  `{"type":"object","properties":{"a":{"type":"string","x-mcp-header":"bad header"}}}`,
			wantErr: "invalid character",
		},
		{
			name: "case-insensitive duplicates are rejected",
			schema: `{"type":"object","properties":{
				"a":{"type":"string","x-mcp-header":"Dup"},
				"b":{"type":"string","x-mcp-header":"dup"}
			}}`,
			wantErr: "duplicate",
		},
		{
			name: "duplicates across nesting levels are rejected",
			schema: `{"type":"object","properties":{
				"a":{"type":"string","x-mcp-header":"Dup"},
				"nested":{"type":"object","properties":{"b":{"type":"string","x-mcp-header":"Dup"}}}
			}}`,
			wantErr: "duplicate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := &Tool{Name: "t", RawInputSchema: json.RawMessage(tt.schema)}
			err := ValidateParamHeaderAnnotations(tool)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestParamHeaderRejectsUnsafeIntegers(t *testing.T) {
	tool := &Tool{
		Name:           "t",
		RawInputSchema: json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer","x-mcp-header":"N"}}}`),
	}

	// Integers beyond the JavaScript safe range, and non-integers, cannot be
	// represented faithfully and so are not mirrored.
	for _, arguments := range []string{
		`{"n": 9007199254740993}`,
		`{"n": 1.5}`,
	} {
		params := json.RawMessage(`{"name":"t","arguments":` + arguments + `}`)
		assert.Empty(t, GenerateParamHeaders(tool, params), "arguments %s", arguments)
	}
}

func TestParamHeaderAnnotationsSurviveTypeArrays(t *testing.T) {
	// JSON Schema allows a type union, and protocol version 2026-07-28 widened
	// input schemas to the full 2020-12 vocabulary. A property using the array
	// form must not disable header handling for the whole tool.
	tool := &Tool{
		Name: "query",
		RawInputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"note":   { "type": ["string", "null"] },
				"region": { "type": "string", "x-mcp-header": "Region" }
			}
		}`),
	}

	bindings := ExtractParamHeaderBindings(tool)
	require.Len(t, bindings, 1, "the annotated property must still be found")
	assert.Equal(t, "Region", bindings[0].Header)

	params := json.RawMessage(`{"name":"query","arguments":{"region":"us-east-1"}}`)
	assert.Equal(t, "us-east-1", GenerateParamHeaders(tool, params)[HeaderParamPrefix+"Region"])

	assert.NoError(t, ValidateParamHeaderAnnotations(tool))
}

func TestParamHeaderAnnotationsRejectedOnNullableTypeUnion(t *testing.T) {
	// A union is only acceptable if every member is a permitted primitive.
	tool := &Tool{
		Name: "query",
		RawInputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"region": { "type": ["string", "null"], "x-mcp-header": "Region" }
			}
		}`),
	}

	err := ValidateParamHeaderAnnotations(tool)
	require.Error(t, err, "null is not a header-representable type")
	assert.Contains(t, err.Error(), "primitive types")
}

func TestParamHeaderAnnotationsToleratesUnreadableSchema(t *testing.T) {
	// A schema that cannot be parsed degrades rather than failing
	// registration, matching this package's policy for schemas that fail to
	// compile. No headers can be derived from it either, so nothing is
	// smuggled past validation.
	tool := &Tool{
		Name:           "broken",
		RawInputSchema: json.RawMessage(`{"type": "object", "properties": {`),
	}

	assert.NoError(t, ValidateParamHeaderAnnotations(tool))
	assert.Empty(t, ExtractParamHeaderBindings(tool))
}
