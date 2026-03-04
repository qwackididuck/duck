package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// Load populates a new instance of T from the configured sources and returns
// it.
//
// Source priority (highest to lowest):
//  1. Environment variables — always win, regardless of file content.
//  2. Config file (JSON or YAML).
//  3. Default values declared via duck:"default=...".
//
// For slice fields populated from environment variables, use duck:"sep=<char>"
// to control how the string value is split (default separator: ",").
// The sep= directive has no effect on values read from JSON or YAML files,
// which must use native array syntax instead.
//
// If env and file both define the same slice field, the env value wins and is
// parsed as a separated string — the file array is ignored entirely.
//
// If a mandatory field is missing and has the panic tag, Load panics.
// If a mandatory field is missing without the panic tag, Load returns an error
// wrapping [ErrMissingMandatory].
func Load[T any](opts ...Option) (T, error) {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	var cfg T

	// Step 1: validate struct tags before doing any I/O.
	if err := validateTags[T](); err != nil {
		return cfg, err
	}

	// Step 2: load file sources into a raw value map.
	raw := map[string]any{}

	for _, src := range o.sources {
		if src == sourceFile {
			filemap, err := loadFile(o.filePath)
			if err != nil {
				return cfg, fmt.Errorf("load config file %q: %w", o.filePath, err)
			}

			maps.Copy(raw, filemap)
		}
	}

	// Step 3: walk struct fields and populate.
	if err := populate(o, &cfg, raw); err != nil {
		return cfg, err
	}

	return cfg, nil
}

// MustLoad is like [Load] but panics on any error.
func MustLoad[T any](opts ...Option) T {
	cfg, err := Load[T](opts...)
	if err != nil {
		panic(fmt.Sprintf("duck/config: MustLoad failed: %v", err))
	}

	return cfg
}

// validateTags checks struct tag coherence before any I/O.
// Currently enforces: sep= is only valid on slice fields.
func validateTags[T any]() error {
	var zero T

	rt := reflect.TypeOf(zero)

	for field := range rt.Fields() {
		meta := parseDuckTag(field.Tag.Get("duck"))

		if meta.sep != "" && field.Type.Kind() != reflect.Slice {
			return fmt.Errorf(
				"%w: field %q has sep= tag but is not a slice (type: %s)",
				ErrInvalidTag, field.Name, field.Type,
			)
		}
	}

	return nil
}

// populate fills the fields of cfgPtr using environment variables, the raw
// file map, and duck tag defaults/validation.
func populate[T any](o *options, cfgPtr *T, raw map[string]any) error {
	rv := reflect.ValueOf(cfgPtr).Elem()
	rt := rv.Type()

	for i := range rt.NumField() {
		field := rt.Field(i)
		fieldVal := rv.Field(i)

		if !fieldVal.CanSet() {
			continue
		}

		meta := parseDuckTag(field.Tag.Get("duck"))

		if err := populateField(o, fieldVal, field, meta, raw); err != nil {
			return err
		}
	}

	return nil
}

// populateField resolves and sets the value for a single struct field.
//
//nolint:gocritic
func populateField(o *options, fieldVal reflect.Value, field reflect.StructField, meta fieldMeta, raw map[string]any) error {
	if envValue, ok := resolveEnv(o, field); ok {
		return setField(fieldVal, envValue, meta, field.Name)
	}

	if fileValue, ok := resolveFile(o, field, raw); ok {
		return setFieldFromAny(fieldVal, fileValue, meta, field.Name)
	}

	if meta.hasDefault {
		return setField(fieldVal, meta.defaultV, meta, field.Name)
	}

	if meta.mandatory {
		msg := buildErrMsg(meta, field.Name)
		if meta.panic {
			panic("duck/config: " + msg)
		}

		return fmt.Errorf("%w: %s", ErrMissingMandatory, msg)
	}

	return nil
}

// resolveEnv returns the env var value for the field if WithEnv is active and
// the variable is set.
//
//nolint:gocritic
func resolveEnv(o *options, field reflect.StructField) (string, bool) {
	for _, src := range o.sources {
		if src == sourceEnv {
			envKey := field.Tag.Get("env")
			if envKey == "" {
				return "", false
			}

			if v := os.Getenv(envKey); v != "" {
				return v, true
			}
		}
	}

	return "", false
}

// resolveFile returns the raw file value for the field if WithFile is active
// and the key exists in the raw map.
//
//nolint:gocritic
func resolveFile(o *options, field reflect.StructField, raw map[string]any) (any, bool) {
	for _, src := range o.sources {
		if src == sourceFile {
			for _, k := range candidateKeys(field) {
				if v, ok := raw[k]; ok {
					return v, true
				}
			}
		}
	}

	return nil, false
}

// setFieldFromAny sets a field from a value that may be a native Go type
// (string, []any, etc.) as decoded from JSON/YAML.
// For slice fields, it handles []any directly without relying on sep=.
func setFieldFromAny(fieldVal reflect.Value, value any, meta fieldMeta, fieldName string) error {
	ft := fieldVal.Type()

	// Native slice from JSON/YAML ([]any) → convert element by element.
	if ft.Kind() == reflect.Slice && ft != byteSliceType {
		items, ok := value.([]any)
		if !ok {
			// Fallback: value is a string (e.g. from a flat file) — use sep= path.
			return setField(fieldVal, fmt.Sprintf("%v", value), meta, fieldName)
		}

		slice := reflect.MakeSlice(ft, 0, len(items))

		for _, item := range items {
			converted, err := convertScalar(ft.Elem(), fmt.Sprintf("%v", item))
			if err != nil {
				return conversionError(meta, fieldName, fmt.Sprintf("%v", item), ft.Elem(), err)
			}

			slice = reflect.Append(slice, converted)
		}

		fieldVal.Set(slice)

		return nil
	}

	// Scalar: convert the value to string then use the standard path.
	return setField(fieldVal, fmt.Sprintf("%v", value), meta, fieldName)
}

// candidateKeys returns the possible map keys for a field when reading from
// a file source. It checks, in order: env tag, json tag, yaml tag, field name.
//
//nolint:gocritic
func candidateKeys(field reflect.StructField) []string {
	keys := []string{}

	for _, tag := range []string{"env", "json", "yaml"} {
		if v := field.Tag.Get(tag); v != "" {
			name := strings.Split(v, ",")[0]
			if name != "" && name != "-" {
				keys = append(keys, name)
			}
		}
	}

	keys = append(keys, field.Name)

	return keys
}

// buildErrMsg returns the error message for a missing mandatory field.
func buildErrMsg(meta fieldMeta, fieldName string) string {
	if meta.errMsg != "" {
		return meta.errMsg
	}

	return fmt.Sprintf("field %q is required but was not set", fieldName)
}

// loadFile reads a JSON or YAML file and returns its contents as a raw value
// map preserving native types (arrays, booleans, numbers).
func loadFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(path))

	switch ext {
	case ".yaml", ".yml":
		return parseYAML(data)
	default:
		return parseJSON(data)
	}
}

// parseJSON decodes a JSON object preserving native types.
func parseJSON(data []byte) (map[string]any, error) {
	raw := map[string]any{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse json: %w", err)
	}

	return raw, nil
}

// parseYAML decodes a YAML document preserving native types.
func parseYAML(data []byte) (map[string]any, error) {
	raw := map[string]any{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}

	return raw, nil
}

// ErrInvalidTag is returned when a struct tag configuration is invalid.
// This is detected at Load() time, before any I/O.
var ErrInvalidTag = errors.New("invalid duck tag")
