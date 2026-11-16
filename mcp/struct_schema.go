package mcp

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// schemaFor generates a JSON schema for T, applying mcp-go struct field tag
// conventions on top of github.com/google/jsonschema-go/jsonschema.
//
// Supported field tags:
//   - jsonschema_description: property description
//   - jsonschema: plain text description, or comma-separated options such as enum=value
func schemaFor[T any]() (*jsonschema.Schema, error) {
	opts := &jsonschema.ForOptions{IgnoreInvalidTypes: true}

	schema, err := jsonschema.For[T](opts)
	if err != nil {
		if !isJSONSchemaTagOptionError(err) || !supportsSchemaTagFallback(reflect.TypeFor[T]()) {
			return nil, err
		}

		schema, err = schemaForStructFields(reflect.TypeFor[T](), opts)
		if err != nil {
			return nil, err
		}
	}

	applyStructFieldTagsToType(reflect.TypeFor[T](), schema)
	return schema, nil
}

func isJSONSchemaTagOptionError(err error) bool {
	return strings.Contains(err.Error(), "tag must not begin with 'WORD='")
}

func supportsSchemaTagFallback(t reflect.Type) bool {
	return supportsSchemaTagFallbackType(t, make(map[reflect.Type]bool))
}

func supportsSchemaTagFallbackType(t reflect.Type, seen map[reflect.Type]bool) bool {
	switch t.Kind() {
	case reflect.Pointer, reflect.Array, reflect.Slice:
		return supportsSchemaTagFallbackType(t.Elem(), seen)
	case reflect.Map:
		return supportsSchemaTagFallbackType(t.Elem(), seen)
	case reflect.Struct:
		if seen[t] {
			return true
		}
		seen[t] = true
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if fieldJSONInfo(field).omit {
				continue
			}
			if tag, ok := field.Tag.Lookup("jsonschema"); ok {
				for option := range strings.SplitSeq(tag, ",") {
					option = strings.TrimSpace(option)
					if strings.Contains(option, "=") && !strings.HasPrefix(option, "enum=") {
						return false
					}
				}
			}
			if !supportsSchemaTagFallbackType(field.Type, seen) {
				return false
			}
		}
	}
	return true
}

var errRecursiveSchemaFallback = errors.New("recursive type cannot use schema tag fallback")

func schemaForStructFields(t reflect.Type, opts *jsonschema.ForOptions) (*jsonschema.Schema, error) {
	state := schemaFallbackState{active: make(map[reflect.Type]bool)}
	return state.schemaForStructFields(t, opts)
}

type schemaFallbackState struct {
	active map[reflect.Type]bool
}

func (s *schemaFallbackState) schemaForStructFields(
	t reflect.Type,
	opts *jsonschema.ForOptions,
) (*jsonschema.Schema, error) {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return jsonschema.ForType(t, opts)
	}
	if s.active[t] {
		return nil, fmt.Errorf("%w: %s", errRecursiveSchemaFallback, t)
	}
	s.active[t] = true
	defer delete(s.active, t)

	schema := &jsonschema.Schema{
		Type:                 "object",
		Properties:           map[string]*jsonschema.Schema{},
		AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}},
	}

	var walk func(reflect.Type) error
	walk = func(structType reflect.Type) error {
		for i := 0; i < structType.NumField(); i++ {
			field := structType.Field(i)
			fieldType := field.Type
			info := fieldJSONInfo(field)
			if info.omit {
				continue
			}
			if field.Anonymous && !info.explicitName {
				anonType := fieldType
				if anonType.Kind() == reflect.Pointer {
					anonType = anonType.Elem()
				}
				if anonType.Kind() == reflect.Struct {
					if s.active[anonType] {
						return fmt.Errorf("%w: %s", errRecursiveSchemaFallback, anonType)
					}
					s.active[anonType] = true
					if err := walk(anonType); err != nil {
						return err
					}
					delete(s.active, anonType)
					continue
				}
			}

			fieldSchema, err := s.schemaForFieldType(fieldType, opts)
			if err != nil {
				if errors.Is(err, errRecursiveSchemaFallback) {
					return err
				}
				if opts.IgnoreInvalidTypes {
					continue
				}
				return err
			}
			if fieldSchema == nil {
				continue
			}

			schema.Properties[info.name] = fieldSchema
			schema.PropertyOrder = append(schema.PropertyOrder, info.name)

			if !info.settings["omitempty"] && !info.settings["omitzero"] {
				schema.Required = append(schema.Required, info.name)
			}
		}
		return nil
	}

	if err := walk(t); err != nil {
		return nil, err
	}
	return schema, nil
}

func (s *schemaFallbackState) schemaForFieldType(
	t reflect.Type,
	opts *jsonschema.ForOptions,
) (*jsonschema.Schema, error) {
	schema, err := jsonschema.ForType(t, opts)
	if err == nil || !isJSONSchemaTagOptionError(err) || !supportsSchemaTagFallback(t) {
		return schema, err
	}

	switch t.Kind() {
	case reflect.Pointer:
		return s.schemaForFieldType(t.Elem(), opts)
	case reflect.Struct:
		return s.schemaForStructFields(t, opts)
	case reflect.Array, reflect.Slice:
		items, itemErr := s.schemaForFieldType(t.Elem(), opts)
		if itemErr != nil {
			return nil, itemErr
		}
		schema := &jsonschema.Schema{Type: "array", Items: items}
		if t.Kind() == reflect.Array {
			schema.MinItems = jsonschema.Ptr(t.Len())
			schema.MaxItems = jsonschema.Ptr(t.Len())
		}
		return schema, nil
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return nil, err
		}
		values, valueErr := s.schemaForFieldType(t.Elem(), opts)
		if valueErr != nil {
			return nil, valueErr
		}
		return &jsonschema.Schema{Type: "object", AdditionalProperties: values}, nil
	default:
		return nil, err
	}
}

func applyStructFieldTags(t reflect.Type, schema *jsonschema.Schema) {
	if schema == nil || schema.Properties == nil {
		return
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}

	var walk func(reflect.Type)
	walk = func(structType reflect.Type) {
		for i := 0; i < structType.NumField(); i++ {
			field := structType.Field(i)
			fieldType := field.Type
			info := fieldJSONInfo(field)
			if info.omit {
				continue
			}
			if field.Anonymous && !info.explicitName {
				anonType := fieldType
				if anonType.Kind() == reflect.Pointer {
					anonType = anonType.Elem()
				}
				if anonType.Kind() == reflect.Struct {
					walk(anonType)
					continue
				}
			}

			prop, ok := schema.Properties[info.name]
			if !ok {
				continue
			}

			description, enums := fieldSchemaAnnotations(field)
			if description != "" {
				prop.Description = description
			}
			if len(enums) > 0 {
				prop.Enum = enums
			}
			applyStructFieldTagsToType(fieldType, prop)
		}
	}

	walk(t)
	applyQuotedFieldTags(t, schema)
}

func applyQuotedFieldTags(t reflect.Type, schema *jsonschema.Schema) {
	type candidate struct {
		field     reflect.StructField
		info      jsonFieldInfo
		depth     int
		ambiguous bool
	}
	fields := make(map[string]candidate)
	active := make(map[reflect.Type]bool)
	var walk func(reflect.Type, int)
	walk = func(t reflect.Type, depth int) {
		if active[t] {
			return
		}
		active[t] = true
		defer delete(active, t)
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			info := fieldJSONTagInfo(field)
			if info.omit {
				continue
			}
			fieldType := field.Type
			if fieldType.Kind() == reflect.Pointer {
				fieldType = fieldType.Elem()
			}
			if field.Anonymous && !info.explicitName && fieldType.Kind() == reflect.Struct {
				walk(fieldType, depth+1)
				continue
			}
			if !field.IsExported() {
				continue
			}
			previous, exists := fields[info.name]
			if exists {
				if previous.depth < depth {
					continue
				}
				if previous.depth == depth {
					if previous.info.explicitName && !info.explicitName {
						continue
					}
					if previous.info.explicitName == info.explicitName {
						previous.ambiguous = true
						fields[info.name] = previous
						continue
					}
				}
			}
			fields[info.name] = candidate{field: field, info: info, depth: depth}
		}
	}
	walk(t, 0)
	for name, field := range fields {
		if prop := schema.Properties[name]; prop != nil && !field.ambiguous && field.info.settings["string"] {
			schema.Properties[name] = quotedFieldSchema(field.field.Type, prop)
		}
	}
}

func quotedFieldSchema(t reflect.Type, schema *jsonschema.Schema) *jsonschema.Schema {
	fieldType := t
	if t.Name() == "" && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	for _, iface := range []reflect.Type{reflect.TypeFor[json.Marshaler](), reflect.TypeFor[encoding.TextMarshaler]()} {
		if t.Implements(iface) || reflect.PointerTo(t).Implements(iface) {
			return schema
		}
	}
	switch t.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.String:
		quoted := &jsonschema.Schema{Type: "string", Description: schema.Description, Enum: schema.Enum}
		if fieldType.Kind() == reflect.Pointer {
			quoted.Type = ""
			quoted.Types = []string{"null", "string"}
		}
		return quoted
	default:
		return schema
	}
}

func applyStructFieldTagsToType(t reflect.Type, schema *jsonschema.Schema) {
	if schema == nil {
		return
	}

	switch t.Kind() {
	case reflect.Pointer:
		applyStructFieldTagsToType(t.Elem(), schema)
	case reflect.Struct:
		applyStructFieldTags(t, schema)
	case reflect.Array, reflect.Slice:
		applyStructFieldTagsToType(t.Elem(), schema.Items)
	case reflect.Map:
		if t.Key().Kind() == reflect.String {
			applyStructFieldTagsToType(t.Elem(), schema.AdditionalProperties)
		}
	}
}

func fieldSchemaAnnotations(field reflect.StructField) (string, []any) {
	description := ""
	var enums []any

	if tag, ok := field.Tag.Lookup("jsonschema_description"); ok {
		description = strings.TrimSpace(tag)
	}

	if tag, ok := field.Tag.Lookup("jsonschema"); ok {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			return description, enums
		}

		parsedDescription, parsedEnums := parseJSONSchemaTag(tag)
		if len(parsedEnums) > 0 {
			enums = parsedEnums
		}
		if description == "" && parsedDescription != "" {
			description = parsedDescription
		}
	}

	return description, enums
}

func parseJSONSchemaTag(tag string) (string, []any) {
	parts := strings.Split(tag, ",")
	var textParts []string
	var enums []any

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if enumValue, ok := strings.CutPrefix(part, "enum="); ok {
			enums = append(enums, enumValue)
			continue
		}

		textParts = append(textParts, part)
	}

	return strings.Join(textParts, ", "), enums
}

type jsonFieldInfo struct {
	omit         bool
	explicitName bool
	name         string
	settings     map[string]bool
}

func fieldJSONInfo(field reflect.StructField) jsonFieldInfo {
	if !field.IsExported() {
		return jsonFieldInfo{omit: true}
	}
	return fieldJSONTagInfo(field)
}

func fieldJSONTagInfo(field reflect.StructField) jsonFieldInfo {
	info := jsonFieldInfo{name: field.Name}
	tag, ok := field.Tag.Lookup("json")
	if !ok {
		return info
	}

	name, rest, found := strings.Cut(tag, ",")
	if name == "-" && !found {
		return jsonFieldInfo{omit: true}
	}
	if name != "" {
		info.name = name
		info.explicitName = true
	}
	if rest != "" {
		info.settings = map[string]bool{}
		for setting := range strings.SplitSeq(rest, ",") {
			info.settings[setting] = true
		}
	}
	return info
}

func schemaForRawMessage[T any]() ([]byte, error) {
	schema, err := schemaFor[T]()
	if err != nil {
		return nil, fmt.Errorf("generate schema: %w", err)
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("marshal schema: %w", err)
	}
	return raw, nil
}
