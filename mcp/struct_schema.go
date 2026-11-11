package mcp

import (
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

	applyStructFieldTags(reflect.TypeFor[T](), schema)
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

type schemaStructField struct {
	field reflect.StructField
	info  jsonFieldInfo
	depth int
	order int
}

func dominantSchemaFields(t reflect.Type) ([]schemaStructField, error) {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, nil
	}

	active := map[reflect.Type]bool{t: true}
	var candidates []schemaStructField
	var walk func(reflect.Type, int) error
	walk = func(structType reflect.Type, depth int) error {
		for i := 0; i < structType.NumField(); i++ {
			field := structType.Field(i)
			info := fieldJSONInfo(field)
			if info.omit {
				continue
			}

			fieldType := field.Type
			if field.Anonymous && !info.explicitName {
				anonType := fieldType
				if anonType.Kind() == reflect.Pointer {
					anonType = anonType.Elem()
				}
				if anonType.Kind() == reflect.Struct {
					if active[anonType] {
						return fmt.Errorf("%w: %s", errRecursiveSchemaFallback, anonType)
					}
					active[anonType] = true
					if err := walk(anonType, depth+1); err != nil {
						return err
					}
					delete(active, anonType)
					continue
				}
			}

			candidates = append(candidates, schemaStructField{
				field: field,
				info:  info,
				depth: depth,
				order: len(candidates),
			})
		}
		return nil
	}
	if err := walk(t, 0); err != nil {
		return nil, err
	}

	byName := make(map[string][]schemaStructField)
	for _, candidate := range candidates {
		byName[candidate.info.name] = append(byName[candidate.info.name], candidate)
	}
	selected := make(map[int]bool, len(byName))
	for _, sameName := range byName {
		shallowest := sameName[0].depth
		for _, candidate := range sameName[1:] {
			if candidate.depth < shallowest {
				shallowest = candidate.depth
			}
		}

		var atDepth []schemaStructField
		var tagged []schemaStructField
		for _, candidate := range sameName {
			if candidate.depth != shallowest {
				continue
			}
			atDepth = append(atDepth, candidate)
			if candidate.info.explicitName {
				tagged = append(tagged, candidate)
			}
		}

		switch {
		case len(atDepth) == 1:
			selected[atDepth[0].order] = true
		case len(tagged) == 1:
			selected[tagged[0].order] = true
		}
	}

	result := make([]schemaStructField, 0, len(selected))
	for _, candidate := range candidates {
		if selected[candidate.order] {
			result = append(result, candidate)
		}
	}
	return result, nil
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

	fields, err := dominantSchemaFields(t)
	if err != nil {
		return nil, err
	}
	for _, selected := range fields {
		fieldSchema, err := s.schemaForFieldType(selected.field.Type, opts)
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

		schema.Properties[selected.info.name] = fieldSchema
		schema.PropertyOrder = append(schema.PropertyOrder, selected.info.name)

		if !selected.info.settings["omitempty"] && !selected.info.settings["omitzero"] {
			schema.Required = append(schema.Required, selected.info.name)
		}
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

	fields, err := dominantSchemaFields(t)
	if err != nil {
		return
	}
	for _, selected := range fields {
		prop, ok := schema.Properties[selected.info.name]
		if !ok {
			continue
		}

		description, enums := fieldSchemaAnnotations(selected.field)
		if description != "" {
			prop.Description = description
		}
		if len(enums) > 0 {
			prop.Enum = enums
		}
		applyStructFieldTagsToType(selected.field.Type, prop)
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
