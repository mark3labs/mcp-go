package mcp

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test AsXXXContent type assertion helpers

func TestAsTextContent(t *testing.T) {
	t.Run("valid TextContent", func(t *testing.T) {
		content := TextContent{Type: ContentTypeText, Text: "hello"}
		result, ok := AsTextContent(content)
		assert.True(t, ok)
		require.NotNil(t, result)
		assert.Equal(t, "hello", result.Text)
	})

	t.Run("invalid type", func(t *testing.T) {
		content := ImageContent{Type: ContentTypeImage, Data: "data"}
		result, ok := AsTextContent(content)
		assert.False(t, ok)
		assert.Nil(t, result)
	})

	t.Run("wrong type string", func(t *testing.T) {
		result, ok := AsTextContent("not a text content")
		assert.False(t, ok)
		assert.Nil(t, result)
	})
}

func TestAsImageContent(t *testing.T) {
	t.Run("valid ImageContent", func(t *testing.T) {
		content := ImageContent{Type: ContentTypeImage, Data: "base64", MIMEType: "image/png"}
		result, ok := AsImageContent(content)
		assert.True(t, ok)
		require.NotNil(t, result)
		assert.Equal(t, "base64", result.Data)
		assert.Equal(t, "image/png", result.MIMEType)
	})

	t.Run("invalid type", func(t *testing.T) {
		content := TextContent{Type: ContentTypeText, Text: "text"}
		result, ok := AsImageContent(content)
		assert.False(t, ok)
		assert.Nil(t, result)
	})
}

func TestAsAudioContent(t *testing.T) {
	t.Run("valid AudioContent", func(t *testing.T) {
		content := AudioContent{Type: ContentTypeAudio, Data: "base64", MIMEType: "audio/mp3"}
		result, ok := AsAudioContent(content)
		assert.True(t, ok)
		require.NotNil(t, result)
		assert.Equal(t, "base64", result.Data)
		assert.Equal(t, "audio/mp3", result.MIMEType)
	})

	t.Run("invalid type", func(t *testing.T) {
		result, ok := AsAudioContent(123)
		assert.False(t, ok)
		assert.Nil(t, result)
	})
}

func TestAsEmbeddedResource(t *testing.T) {
	t.Run("valid EmbeddedResource", func(t *testing.T) {
		resource := TextResourceContents{URI: "file:///test.txt", Text: "content"}
		content := EmbeddedResource{Type: ContentTypeResource, Resource: resource}
		result, ok := AsEmbeddedResource(content)
		assert.True(t, ok)
		require.NotNil(t, result)
		assert.Equal(t, resource, result.Resource)
	})

	t.Run("invalid type", func(t *testing.T) {
		result, ok := AsEmbeddedResource(nil)
		assert.False(t, ok)
		assert.Nil(t, result)
	})
}

func TestAsTextResourceContents(t *testing.T) {
	t.Run("valid TextResourceContents", func(t *testing.T) {
		content := TextResourceContents{URI: "file:///test.txt", Text: "hello"}
		result, ok := AsTextResourceContents(content)
		assert.True(t, ok)
		require.NotNil(t, result)
		assert.Equal(t, "hello", result.Text)
	})

	t.Run("invalid type", func(t *testing.T) {
		content := BlobResourceContents{URI: "file:///test.bin", Blob: "data"}
		result, ok := AsTextResourceContents(content)
		assert.False(t, ok)
		assert.Nil(t, result)
	})
}

func TestAsBlobResourceContents(t *testing.T) {
	t.Run("valid BlobResourceContents", func(t *testing.T) {
		content := BlobResourceContents{URI: "file:///test.bin", Blob: "base64"}
		result, ok := AsBlobResourceContents(content)
		assert.True(t, ok)
		require.NotNil(t, result)
		assert.Equal(t, "base64", result.Blob)
	})

	t.Run("invalid type", func(t *testing.T) {
		result, ok := AsBlobResourceContents([]byte{1, 2, 3})
		assert.False(t, ok)
		assert.Nil(t, result)
	})
}

// Test NewJSONRPCError and NewJSONRPCErrorDetails

func TestNewJSONRPCError(t *testing.T) {
	id := NewRequestId(123)
	code := METHOD_NOT_FOUND
	message := "Method not found"
	data := map[string]any{"method": "unknown"}

	result := NewJSONRPCError(id, code, message, data)

	assert.Equal(t, JSONRPC_VERSION, result.JSONRPC)
	assert.Equal(t, id, result.ID)
	assert.Equal(t, code, result.Error.Code)
	assert.Equal(t, message, result.Error.Message)
	assert.Equal(t, data, result.Error.Data)
}

func TestNewJSONRPCErrorDetails(t *testing.T) {
	code := INVALID_PARAMS
	message := "Invalid parameters"
	data := "Additional error info"

	result := NewJSONRPCErrorDetails(code, message, data)

	assert.Equal(t, code, result.Code)
	assert.Equal(t, message, result.Message)
	assert.Equal(t, data, result.Data)
}

// Test helper content creation functions

func TestNewAudioContent(t *testing.T) {
	result := NewAudioContent("audiodata", "audio/mp3")

	assert.Equal(t, ContentTypeAudio, result.Type)
	assert.Equal(t, "audiodata", result.Data)
	assert.Equal(t, "audio/mp3", result.MIMEType)
}

func TestNewResourceLink(t *testing.T) {
	result := NewResourceLink("file:///test.txt", "test.txt", "A test file", "text/plain")

	assert.Equal(t, ContentTypeLink, result.Type)
	assert.Equal(t, "file:///test.txt", result.URI)
	assert.Equal(t, "test.txt", result.Name)
	assert.Equal(t, "A test file", result.Description)
	assert.Equal(t, "text/plain", result.MIMEType)
}

func TestNewEmbeddedResource(t *testing.T) {
	resource := TextResourceContents{URI: "file:///test.txt", Text: "content"}
	result := NewEmbeddedResource(resource)

	assert.Equal(t, ContentTypeResource, result.Type)
	assert.Equal(t, resource, result.Resource)
}

// Test ParseResourceContents

func TestParseResourceContents(t *testing.T) {
	t.Run("text resource", func(t *testing.T) {
		contentMap := map[string]any{
			"uri":      "file:///test.txt",
			"mimeType": "text/plain",
			"text":     "hello world",
		}

		result, err := ParseResourceContents(contentMap)
		require.NoError(t, err)

		textRes, ok := result.(TextResourceContents)
		require.True(t, ok)
		assert.Equal(t, "file:///test.txt", textRes.URI)
		assert.Equal(t, "text/plain", textRes.MIMEType)
		assert.Equal(t, "hello world", textRes.Text)
	})

	t.Run("blob resource", func(t *testing.T) {
		contentMap := map[string]any{
			"uri":      "file:///test.bin",
			"mimeType": "application/octet-stream",
			"blob":     "base64data",
		}

		result, err := ParseResourceContents(contentMap)
		require.NoError(t, err)

		blobRes, ok := result.(BlobResourceContents)
		require.True(t, ok)
		assert.Equal(t, "file:///test.bin", blobRes.URI)
		assert.Equal(t, "application/octet-stream", blobRes.MIMEType)
		assert.Equal(t, "base64data", blobRes.Blob)
	})

	t.Run("missing uri", func(t *testing.T) {
		contentMap := map[string]any{
			"text": "hello",
		}

		_, err := ParseResourceContents(contentMap)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "uri is missing")
	})

	t.Run("no text or blob", func(t *testing.T) {
		contentMap := map[string]any{
			"uri": "file:///test",
		}

		_, err := ParseResourceContents(contentMap)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported resource type")
	})
}

// Test ParseGetPromptResult with malformed JSON

func TestParseGetPromptResult_Errors(t *testing.T) {
	t.Run("nil raw message", func(t *testing.T) {
		_, err := ParseGetPromptResult(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nil")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		raw := json.RawMessage(`{invalid json}`)
		_, err := ParseGetPromptResult(&raw)
		assert.Error(t, err)
	})

	t.Run("messages not array", func(t *testing.T) {
		raw := json.RawMessage(`{"messages": "not an array"}`)
		_, err := ParseGetPromptResult(&raw)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not an array")
	})

	t.Run("message not object", func(t *testing.T) {
		raw := json.RawMessage(`{"messages": ["not an object"]}`)
		_, err := ParseGetPromptResult(&raw)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not an object")
	})

	t.Run("unsupported role", func(t *testing.T) {
		raw := json.RawMessage(`{
			"messages": [{
				"role": "system",
				"content": {"type": "text", "text": "hello"}
			}]
		}`)
		_, err := ParseGetPromptResult(&raw)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported role")
	})

	t.Run("content not object", func(t *testing.T) {
		raw := json.RawMessage(`{
			"messages": [{
				"role": "user",
				"content": "not an object"
			}]
		}`)
		_, err := ParseGetPromptResult(&raw)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not an object")
	})
}

// Test ParseCallToolResult with malformed JSON

func TestParseCallToolResult_Errors(t *testing.T) {
	t.Run("nil raw message", func(t *testing.T) {
		_, err := ParseCallToolResult(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nil")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		raw := json.RawMessage(`{invalid}`)
		_, err := ParseCallToolResult(&raw)
		assert.Error(t, err)
	})

	t.Run("missing content", func(t *testing.T) {
		raw := json.RawMessage(`{"isError": false}`)
		_, err := ParseCallToolResult(&raw)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "content is missing")
	})

	t.Run("content not array", func(t *testing.T) {
		raw := json.RawMessage(`{"content": "not an array"}`)
		_, err := ParseCallToolResult(&raw)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not an array")
	})

	t.Run("content item not object", func(t *testing.T) {
		raw := json.RawMessage(`{"content": ["not an object"]}`)
		_, err := ParseCallToolResult(&raw)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not an object")
	})
}

// Test ParseReadResourceResult with malformed JSON

func TestParseReadResourceResult_Errors(t *testing.T) {
	t.Run("nil raw message", func(t *testing.T) {
		_, err := ParseReadResourceResult(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nil")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		raw := json.RawMessage(`{bad json}`)
		_, err := ParseReadResourceResult(&raw)
		assert.Error(t, err)
	})

	t.Run("missing contents", func(t *testing.T) {
		raw := json.RawMessage(`{}`)
		_, err := ParseReadResourceResult(&raw)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "contents is missing")
	})

	t.Run("contents not array", func(t *testing.T) {
		raw := json.RawMessage(`{"contents": "not an array"}`)
		_, err := ParseReadResourceResult(&raw)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not an array")
	})

	t.Run("content item not object", func(t *testing.T) {
		raw := json.RawMessage(`{"contents": [123]}`)
		_, err := ParseReadResourceResult(&raw)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not an object")
	})
}

// Test ParseStringMap

func TestParseStringMap(t *testing.T) {
	req := CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"valid_map": map[string]any{
			"key1": "value1",
			"key2": 123,
		},
		"not_a_map": "string value",
	}

	t.Run("valid map", func(t *testing.T) {
		result := ParseStringMap(req, "valid_map", nil)
		require.NotNil(t, result)
		assert.Equal(t, "value1", result["key1"])
		assert.Equal(t, 123, result["key2"])
	})

	t.Run("invalid type returns empty map", func(t *testing.T) {
		defaultMap := map[string]any{"default": "value"}
		result := ParseStringMap(req, "not_a_map", defaultMap)
		// cast.ToStringMap returns empty map when it can't convert
		assert.Equal(t, map[string]any{}, result)
	})

	t.Run("missing key returns converted default", func(t *testing.T) {
		defaultMap := map[string]any{"default": "value"}
		result := ParseStringMap(req, "missing", defaultMap)
		// ParseArgument returns the default, which is then converted by cast.ToStringMap
		assert.Equal(t, defaultMap, result)
	})
}

// Test ExtractMap and ExtractString

func TestExtractMap(t *testing.T) {
	data := map[string]any{
		"nested": map[string]any{
			"key": "value",
		},
		"not_map": "string",
	}

	t.Run("valid map", func(t *testing.T) {
		result := ExtractMap(data, "nested")
		require.NotNil(t, result)
		assert.Equal(t, "value", result["key"])
	})

	t.Run("not a map", func(t *testing.T) {
		result := ExtractMap(data, "not_map")
		assert.Nil(t, result)
	})

	t.Run("missing key", func(t *testing.T) {
		result := ExtractMap(data, "missing")
		assert.Nil(t, result)
	})
}

func TestExtractString(t *testing.T) {
	data := map[string]any{
		"string_val": "hello",
		"int_val":    123,
	}

	t.Run("valid string", func(t *testing.T) {
		result := ExtractString(data, "string_val")
		assert.Equal(t, "hello", result)
	})

	t.Run("not a string", func(t *testing.T) {
		result := ExtractString(data, "int_val")
		assert.Equal(t, "", result)
	})

	t.Run("missing key", func(t *testing.T) {
		result := ExtractString(data, "missing")
		assert.Equal(t, "", result)
	})
}

// Test all ParseXXX functions

func TestParseIntVariants(t *testing.T) {
	req := CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"valid": "42",
	}

	t.Run("ParseInt32", func(t *testing.T) {
		result := ParseInt32(req, "valid", 0)
		assert.Equal(t, int32(42), result)

		result = ParseInt32(req, "missing", 10)
		assert.Equal(t, int32(10), result)
	})

	t.Run("ParseInt16", func(t *testing.T) {
		result := ParseInt16(req, "valid", 0)
		assert.Equal(t, int16(42), result)
	})

	t.Run("ParseInt8", func(t *testing.T) {
		result := ParseInt8(req, "valid", 0)
		assert.Equal(t, int8(42), result)
	})

	t.Run("ParseUInt", func(t *testing.T) {
		result := ParseUInt(req, "valid", 0)
		assert.Equal(t, uint(42), result)
	})

	t.Run("ParseUInt64", func(t *testing.T) {
		result := ParseUInt64(req, "valid", 0)
		assert.Equal(t, uint64(42), result)
	})

	t.Run("ParseUInt32", func(t *testing.T) {
		result := ParseUInt32(req, "valid", 0)
		assert.Equal(t, uint32(42), result)
	})

	t.Run("ParseUInt16", func(t *testing.T) {
		result := ParseUInt16(req, "valid", 0)
		assert.Equal(t, uint16(42), result)
	})

	t.Run("ParseUInt8", func(t *testing.T) {
		result := ParseUInt8(req, "valid", 0)
		assert.Equal(t, uint8(42), result)
	})
}

func TestParseFloatVariants(t *testing.T) {
	req := CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"valid": "3.14",
	}

	t.Run("ParseFloat32", func(t *testing.T) {
		result := ParseFloat32(req, "valid", 0.0)
		assert.InDelta(t, float32(3.14), result, 0.001)

		result = ParseFloat32(req, "missing", 1.5)
		assert.Equal(t, float32(1.5), result)
	})

	t.Run("ParseFloat64", func(t *testing.T) {
		result := ParseFloat64(req, "valid", 0.0)
		assert.InDelta(t, 3.14, result, 0.001)
	})
}

func TestParseString(t *testing.T) {
	req := CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"valid": "hello",
		"int":   123,
	}

	t.Run("valid string", func(t *testing.T) {
		result := ParseString(req, "valid", "")
		assert.Equal(t, "hello", result)
	})

	t.Run("converts int to string", func(t *testing.T) {
		result := ParseString(req, "int", "")
		assert.Equal(t, "123", result)
	})

	t.Run("missing returns default", func(t *testing.T) {
		result := ParseString(req, "missing", "default")
		assert.Equal(t, "default", result)
	})
}

// Test ToBoolPtr

func TestToBoolPtr(t *testing.T) {
	t.Run("true", func(t *testing.T) {
		result := ToBoolPtr(true)
		require.NotNil(t, result)
		assert.True(t, *result)
	})

	t.Run("false", func(t *testing.T) {
		result := ToBoolPtr(false)
		require.NotNil(t, result)
		assert.False(t, *result)
	})
}

// Test NewToolResultJSON with error

func TestNewToolResultJSON_Error(t *testing.T) {
	// Create a type that can't be marshaled
	type BadType struct {
		Func func() // functions can't be marshaled
	}

	_, err := NewToolResultJSON(BadType{Func: func() {}})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unable to marshal JSON")
}

// Test FormatNumberResult

func TestFormatNumberResult(t *testing.T) {
	result := FormatNumberResult(42.5678)

	require.Len(t, result.Content, 1)
	textContent, ok := result.Content[0].(TextContent)
	require.True(t, ok)
	assert.Equal(t, "42.57", textContent.Text)
}

// Test NewToolResultErrorFromErr with nil error

func TestNewToolResultErrorFromErr_NilError(t *testing.T) {
	result := NewToolResultErrorFromErr("test error", nil)

	assert.True(t, result.IsError)
	require.Len(t, result.Content, 1)
	textContent, ok := result.Content[0].(TextContent)
	require.True(t, ok)
	assert.Equal(t, "test error", textContent.Text)
}

func TestNewToolResultErrorFromErr_WithError(t *testing.T) {
	result := NewToolResultErrorFromErr("test error", errors.New("underlying error"))

	assert.True(t, result.IsError)
	require.Len(t, result.Content, 1)
	textContent, ok := result.Content[0].(TextContent)
	require.True(t, ok)
	assert.Contains(t, textContent.Text, "test error")
	assert.Contains(t, textContent.Text, "underlying error")
}
