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
	state := schemaFallbackState{active: make(map[reflect.Type]bool)}

	schema, err := jsonschema.For[T](opts)
	if err != nil {
		if !isJSONSchemaTagOptionError(err) || !supportsSchemaTagFallback(reflect.TypeFor[T]()) {
			return nil, err
		}
		schema, err = state.schemaForFieldType(reflect.TypeFor[T](), opts)
	} else {
		schema, err = state.reconcileNativeSchema(reflect.TypeFor[T](), schema, opts)
	}
	if err != nil {
		return nil, err
	}

	applyStructFieldTagsToType(reflect.TypeFor[T](), schema, applyStructFieldTags)
	applyStructFieldTagsToType(reflect.TypeFor[T](), schema, applyQuotedFieldTags)
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
			fieldType := field.Type
			if fieldType.Kind() == reflect.Pointer {
				fieldType = fieldType.Elem()
			}
			if !field.IsExported() && (!field.Anonymous || fieldType.Kind() != reflect.Struct) {
				continue
			}
			if fieldJSONTagInfo(field).omit {
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
	fields, err := selectedJSONFields(t)
	if err != nil {
		return nil, err
	}
	return s.schemaForSelectedFields(t, fields, nil, opts)
}

func (s *schemaFallbackState) schemaForSelectedFields(
	t reflect.Type,
	fields []jsonSchemaField,
	nativeProperties map[string]*jsonschema.Schema,
	opts *jsonschema.ForOptions,
) (*jsonschema.Schema, error) {
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

	for _, field := range fields {
		var fieldSchema *jsonschema.Schema
		var err error
		if native := nativeProperties[field.info.name]; native != nil {
			fieldSchema, err = s.reconcileNativeSchema(field.field.Type, native, opts)
		} else {
			fieldSchema, err = s.schemaForFieldType(field.field.Type, opts)
		}
		if err != nil {
			if errors.Is(err, errRecursiveSchemaFallback) {
				return nil, err
			}
			if opts.IgnoreInvalidTypes {
				continue
			}
			return nil, err
		}
		if fieldSchema == nil {
			continue
		}

		schema.Properties[field.info.name] = fieldSchema
		schema.PropertyOrder = append(schema.PropertyOrder, field.info.name)
		if !field.info.settings["omitempty"] && !field.info.settings["omitzero"] {
			schema.Required = append(schema.Required, field.info.name)
		}
	}

	return schema, nil
}

func (s *schemaFallbackState) schemaForFieldType(
	t reflect.Type,
	opts *jsonschema.ForOptions,
) (*jsonschema.Schema, error) {
	schema, err := jsonschema.ForType(t, opts)
	if err == nil {
		return s.reconcileNativeSchema(t, schema, opts)
	}
	if !isJSONSchemaTagOptionError(err) || !supportsSchemaTagFallback(t) {
		return schema, err
	}

	switch t.Kind() {
	case reflect.Pointer:
		schema, err := s.schemaForFieldType(t.Elem(), opts)
		if err == nil && schema.Type != "" {
			schema.Types = []string{"null", schema.Type}
			schema.Type = ""
		}
		return schema, err
	case reflect.Struct:
		return s.schemaForStructFields(t, opts)
	case reflect.Array, reflect.Slice:
		items, itemErr := s.schemaForFieldType(t.Elem(), opts)
		if itemErr != nil {
			return nil, itemErr
		}
		schema := &jsonschema.Schema{Type: "array", Items: items}
		if t.Kind() == reflect.Slice {
			schema.Type = ""
			schema.Types = []string{"null", "array"}
		}
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
		return &jsonschema.Schema{Types: []string{"null", "object"}, AdditionalProperties: values}, nil
	default:
		return nil, err
	}
}

// reconcileNativeSchema preserves native leaf schemas and rebuilds struct
// properties only when Go-name visibility disagrees with JSON field selection.
func (s *schemaFallbackState) reconcileNativeSchema(
	t reflect.Type,
	schema *jsonschema.Schema,
	opts *jsonschema.ForOptions,
) (*jsonschema.Schema, error) {
	if schema == nil || hasCustomJSONEncoding(t) {
		return schema, nil
	}
	var err error
	switch t.Kind() {
	case reflect.Pointer:
		return s.reconcileNativeSchema(t.Elem(), schema, opts)
	case reflect.Struct:
		if schema.Properties == nil {
			return schema, nil
		}
		// Native inference already accepted this type; recursive embeddings
		// add no new visible fields and need not use tag-fallback restrictions.
		fields, _ := selectedJSONFields(t)
		native, matches, selectionErr := nativeJSONFieldSelection(t, fields, schema.Properties, opts)
		if selectionErr != nil {
			return nil, selectionErr
		}
		if !matches {
			preserved := make(map[string]*jsonschema.Schema, len(fields))
			for _, field := range fields {
				source, ok := native[field.info.name]
				if ok && sameJSONField(source, field.field) {
					preserved[field.info.name] = schema.Properties[field.info.name]
				}
			}
			rebuilt, buildErr := s.schemaForSelectedFields(t, fields, preserved, opts)
			if buildErr != nil {
				return nil, buildErr
			}
			rebuilt.Type, rebuilt.Types = schema.Type, schema.Types
			rebuilt.Description = schema.Description
			return rebuilt, nil
		}
		for _, field := range fields {
			prop, propErr := s.reconcileNativeSchema(field.field.Type, schema.Properties[field.info.name], opts)
			if propErr != nil {
				return nil, propErr
			}
			if prop != nil {
				schema.Properties[field.info.name] = prop
			}
		}
	case reflect.Array, reflect.Slice:
		schema.Items, err = s.reconcileNativeSchema(t.Elem(), schema.Items, opts)
	case reflect.Map:
		if t.Key().Kind() == reflect.String {
			schema.AdditionalProperties, err = s.reconcileNativeSchema(t.Elem(), schema.AdditionalProperties, opts)
		}
	}
	return schema, err
}

func nativeJSONFieldSelection(
	t reflect.Type,
	fields []jsonSchemaField,
	properties map[string]*jsonschema.Schema,
	opts *jsonschema.ForOptions,
) (map[string]reflect.StructField, bool, error) {
	native := make(map[string]reflect.StructField, len(fields))
	matches := true
	for _, field := range reflect.VisibleFields(t) {
		info := fieldJSONInfo(field)
		if field.Anonymous || info.omit {
			continue
		}
		if _, duplicate := native[info.name]; duplicate {
			matches = false
			if opts.IgnoreInvalidTypes && properties[info.name] != nil {
				// Ignored fields do not overwrite earlier native properties.
				candidate, err := jsonschema.ForType(field.Type, opts)
				if err != nil {
					return nil, false, err
				}
				if candidate == nil {
					continue
				}
			}
		}
		native[info.name] = field
	}
	if len(native) != len(fields) {
		matches = false
	}
	for _, field := range fields {
		got, ok := native[field.info.name]
		if !ok || !sameJSONField(got, field.field) {
			matches = false
		}
	}
	return native, matches, nil
}

func sameJSONField(a, b reflect.StructField) bool {
	return a.Name == b.Name && a.Type == b.Type && a.Tag == b.Tag
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

	active := make(map[reflect.Type]bool)
	var walk func(reflect.Type)
	walk = func(structType reflect.Type) {
		if active[structType] {
			return
		}
		active[structType] = true
		defer delete(active, structType)
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
			applyStructFieldTagsToType(fieldType, prop, applyStructFieldTags)
		}
	}

	walk(t)
}

type jsonSchemaField struct {
	field     reflect.StructField
	info      jsonFieldInfo
	depth     int
	ambiguous bool
}

// selectedJSONFields resolves JSON field visibility before either constructing
// fallback properties or applying the string option to their schemas.
func selectedJSONFields(t reflect.Type) ([]jsonSchemaField, error) {
	fields := make(map[string]jsonSchemaField)
	var names []string
	var cycleErr error
	active := make(map[reflect.Type]bool)
	var walk func(reflect.Type, int)
	walk = func(t reflect.Type, depth int) {
		if active[t] {
			cycleErr = fmt.Errorf("%w: %s", errRecursiveSchemaFallback, t)
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
			if !field.IsExported() && (!field.Anonymous || fieldType.Kind() != reflect.Struct) {
				continue
			}
			previous, exists := fields[info.name]
			if !exists {
				names = append(names, info.name)
			}
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
			fields[info.name] = jsonSchemaField{field: field, info: info, depth: depth}
		}
	}
	walk(t, 0)

	var selected []jsonSchemaField
	for _, name := range names {
		if field := fields[name]; !field.ambiguous {
			selected = append(selected, field)
		}
	}
	return selected, cycleErr
}

func applyQuotedFieldTags(t reflect.Type, schema *jsonschema.Schema) {
	if schema == nil || schema.Properties == nil {
		return
	}
	// Native inference can accept recursive embeddings; selection still returns their visible fields.
	fields, _ := selectedJSONFields(t)
	for _, field := range fields {
		prop := schema.Properties[field.info.name]
		if prop == nil {
			continue
		}
		applyStructFieldTagsToType(field.field.Type, prop, applyQuotedFieldTags)
		if field.info.settings["string"] {
			schema.Properties[field.info.name] = quotedFieldSchema(field.field.Type, prop)
		}
	}
}

func hasCustomJSONEncoding(t reflect.Type) bool {
	for _, iface := range []reflect.Type{reflect.TypeFor[json.Marshaler](), reflect.TypeFor[encoding.TextMarshaler]()} {
		if t.Implements(iface) || reflect.PointerTo(t).Implements(iface) {
			return true
		}
	}
	return false
}

func quotedFieldSchema(t reflect.Type, schema *jsonschema.Schema) *jsonschema.Schema {
	fieldType := t
	if t.Name() == "" && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if hasCustomJSONEncoding(t) {
		return schema
	}
	switch t.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.String:
		quoted := &jsonschema.Schema{Type: "string", Description: schema.Description, Enum: schema.Enum}
		if t.Kind() == reflect.String && t != reflect.TypeFor[json.Number]() && len(schema.Enum) > 0 {
			quoted.Enum = make([]any, len(schema.Enum))
			for i, value := range schema.Enum {
				quoted.Enum[i] = value
				if text, ok := value.(string); ok {
					encoded, _ := json.Marshal(text) // Marshaling a string cannot fail.
					quoted.Enum[i] = string(encoded)
				}
			}
		}
		if fieldType.Kind() == reflect.Pointer {
			quoted.Type = ""
			quoted.Types = []string{"null", "string"}
		}
		return quoted
	default:
		return schema
	}
}

func applyStructFieldTagsToType(t reflect.Type, schema *jsonschema.Schema, applyTags func(reflect.Type, *jsonschema.Schema)) {
	if schema == nil {
		return
	}

	switch t.Kind() {
	case reflect.Pointer:
		applyStructFieldTagsToType(t.Elem(), schema, applyTags)
	case reflect.Struct:
		applyTags(t, schema)
	case reflect.Array, reflect.Slice:
		applyStructFieldTagsToType(t.Elem(), schema.Items, applyTags)
	case reflect.Map:
		if t.Key().Kind() == reflect.String {
			applyStructFieldTagsToType(t.Elem(), schema.AdditionalProperties, applyTags)
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
