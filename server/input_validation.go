// Package server provides MCP (Model Context Protocol) server implementations.
package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/mark3labs/mcp-go/mcp"
)

// inputSchemaValidator compiles and caches JSON Schema validators for tool
// input schemas. Compilation is performed lazily on first use and the
// resulting schema is cached for the lifetime of the server.
//
// The cache is two-level: tool name -> schema digest -> compiled entry. Two
// sessions can expose the same tool name with different schemas (via
// SessionWithTools) and a global tool can be re-registered with a different
// schema; each distinct schema is compiled once. Name-level invalidation
// drops every entry registered for that name in a single map delete, which
// keeps DeleteTools / SetTools cleanup cheap.
type inputSchemaValidator struct {
	mu     sync.RWMutex
	cached map[string]map[string]*cachedToolSchema
}

// cachedToolSchema records the result of compiling a single (toolName,
// schemaDigest) pair. compileErr is stored alongside the compiled schema so
// that a schema that fails to compile is not recompiled on every call.
type cachedToolSchema struct {
	compiled   *jsonschema.Schema
	compileErr error
}

// newInputSchemaValidator returns a fresh validator with an empty cache.
func newInputSchemaValidator() *inputSchemaValidator {
	return &inputSchemaValidator{
		cached: make(map[string]map[string]*cachedToolSchema),
	}
}

// validate validates raw arguments against the tool's input schema. If the
// schema cannot be compiled, validation is treated as a no-op so that broken
// schemas never make tool calls fail unexpectedly. The boolean return reports
// whether validation actually ran; the error is set only when validation ran
// and the arguments did not satisfy the schema.
func (v *inputSchemaValidator) validate(tool mcp.Tool, rawArgs any) (bool, error) {
	schemaJSON, ok := schemaJSONFor(tool)
	if !ok {
		return false, nil
	}

	compiled, err := v.lookupOrCompile(tool.Name, schemaJSON)
	if err != nil {
		return false, nil
	}

	value, err := normalizeArgumentsForValidation(rawArgs)
	if err != nil {
		return true, fmt.Errorf("invalid arguments: %w", err)
	}

	if err := compiled.Validate(value); err != nil {
		return true, formatValidationError(err)
	}
	return true, nil
}

// invalidate drops cached compilations for the given tool names. Used when
// tools are re-registered or removed so that stale schemas are not reused.
func (v *inputSchemaValidator) invalidate(names ...string) {
	if v == nil || len(names) == 0 {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, name := range names {
		delete(v.cached, name)
	}
}

// invalidateAll drops the entire cache.
func (v *inputSchemaValidator) invalidateAll() {
	if v == nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.cached = make(map[string]map[string]*cachedToolSchema)
}

func (v *inputSchemaValidator) lookupOrCompile(toolName string, schemaJSON []byte) (*jsonschema.Schema, error) {
	digest := schemaDigest(schemaJSON)

	v.mu.RLock()
	if perName, ok := v.cached[toolName]; ok {
		if entry, ok := perName[digest]; ok {
			v.mu.RUnlock()
			return entry.compiled, entry.compileErr
		}
	}
	v.mu.RUnlock()

	v.mu.Lock()
	defer v.mu.Unlock()
	// Re-check under the write lock in case another goroutine compiled it.
	perName, ok := v.cached[toolName]
	if !ok {
		perName = make(map[string]*cachedToolSchema)
		v.cached[toolName] = perName
	}
	if entry, ok := perName[digest]; ok {
		return entry.compiled, entry.compileErr
	}

	compiled, compileErr := compileSchema(toolName, schemaJSON)
	perName[digest] = &cachedToolSchema{
		compiled:   compiled,
		compileErr: compileErr,
	}
	return compiled, compileErr
}

// schemaDigest returns a stable hex-encoded SHA-256 of the canonical schema
// bytes. We use a digest rather than the raw bytes as a map key so the cache
// stays cheap to look up even for large schemas; collisions are not a
// security concern because both sides of the comparison are produced by the
// same mcp-go server process.
func schemaDigest(schemaJSON []byte) string {
	sum := sha256.Sum256(schemaJSON)
	return hex.EncodeToString(sum[:])
}

// schemaJSONFor returns the JSON representation of the tool's input schema.
// It prefers RawInputSchema when set, otherwise marshals the structured
// InputSchema. The boolean return is false when the tool has no usable input
// schema (e.g. an empty structured schema with no properties).
func schemaJSONFor(tool mcp.Tool) ([]byte, bool) {
	if len(tool.RawInputSchema) > 0 {
		return tool.RawInputSchema, true
	}
	if tool.InputSchema.Type == "" && len(tool.InputSchema.Properties) == 0 && len(tool.InputSchema.Required) == 0 && tool.InputSchema.AdditionalProperties == nil {
		return nil, false
	}
	data, err := json.Marshal(tool.InputSchema)
	if err != nil {
		return nil, false
	}
	return data, true
}

// compileSchema parses a JSON Schema document and compiles it for the given
// tool. The schema is registered against an opaque virtual URL so that
// "$ref" resolution works without touching the filesystem or network.
func compileSchema(toolName string, schemaJSON []byte) (*jsonschema.Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
	if err != nil {
		return nil, fmt.Errorf("decode tool %q input schema: %w", toolName, err)
	}
	resourceURL := fmt.Sprintf("mem:///mcp-go/tools/%s/input-schema.json", toolName)
	c := jsonschema.NewCompiler()
	if err := c.AddResource(resourceURL, doc); err != nil {
		return nil, fmt.Errorf("register tool %q input schema: %w", toolName, err)
	}
	compiled, err := c.Compile(resourceURL)
	if err != nil {
		return nil, fmt.Errorf("compile tool %q input schema: %w", toolName, err)
	}
	return compiled, nil
}

// normalizeArgumentsForValidation converts the raw arguments value (which may
// be a json.RawMessage, a map, or already-decoded slice/scalars) into the
// loosely-typed representation that the JSON Schema validator expects. Missing
// arguments are normalised to an empty object so that schemas can still
// validate `required` constraints sensibly.
func normalizeArgumentsForValidation(rawArgs any) (any, error) {
	if rawArgs == nil {
		return map[string]any{}, nil
	}
	if raw, ok := rawArgs.(json.RawMessage); ok {
		if len(bytes.TrimSpace(raw)) == 0 {
			return map[string]any{}, nil
		}
		return jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	}
	encoded, err := json.Marshal(rawArgs)
	if err != nil {
		return nil, err
	}
	return jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
}

// formatValidationError turns the validator's structured error into a single
// flat message that's friendly for an LLM to read. Each individual constraint
// violation is rendered as `<path>: <message>` and joined with semicolons.
func formatValidationError(err error) error {
	var verr *jsonschema.ValidationError
	if !errors.As(err, &verr) {
		return fmt.Errorf("input schema validation failed: %w", err)
	}
	parts := collectValidationMessages(verr)
	if len(parts) == 0 {
		return fmt.Errorf("input schema validation failed: %s", verr.Error())
	}
	return fmt.Errorf("input schema validation failed: %s", strings.Join(parts, "; "))
}

// collectValidationMessages walks the leaf causes of a validation error tree
// and produces one message per leaf. Non-leaf nodes don't carry useful
// information for the model (they're aggregator nodes like "anyOf failed").
func collectValidationMessages(verr *jsonschema.ValidationError) []string {
	if verr == nil {
		return nil
	}
	var out []string
	if len(verr.Causes) == 0 {
		out = append(out, formatLeafMessage(verr))
		return out
	}
	for _, cause := range verr.Causes {
		out = append(out, collectValidationMessages(cause)...)
	}
	return out
}

func formatLeafMessage(verr *jsonschema.ValidationError) string {
	location := jsonPointer(verr.InstanceLocation)
	if location == "" {
		location = "<root>"
	}
	return fmt.Sprintf("%s: %s", location, verr.ErrorKind)
}

// jsonPointer renders a list of path segments as a RFC 6901 JSON Pointer.
// Empty paths render as the empty string (the validator's convention for the
// root document) which the caller substitutes for "<root>".
func jsonPointer(segments []string) string {
	if len(segments) == 0 {
		return ""
	}
	var b strings.Builder
	for _, seg := range segments {
		b.WriteByte('/')
		b.WriteString(escapeJSONPointer(seg))
	}
	return b.String()
}

func escapeJSONPointer(seg string) string {
	seg = strings.ReplaceAll(seg, "~", "~0")
	seg = strings.ReplaceAll(seg, "/", "~1")
	return seg
}

// validationToolResult builds the SEP-1303 compliant tool execution error
// surfaced when input schema validation fails. The error message is duplicated
// into the structured field so that clients with structured-content support
// can render it directly.
func validationToolResult(err error) *mcp.CallToolResult {
	msg := err.Error()
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: msg,
			},
		},
		IsError: true,
	}
}
