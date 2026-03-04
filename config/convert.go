package config

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

var durationType = reflect.TypeFor[time.Duration]()
var byteSliceType = reflect.TypeFor[[]byte]()

// setField converts the string value and sets it on fieldVal.
// It handles all supported scalar types, []byte, and slices of scalar types.
// On conversion failure it either panics (if meta.panic) or returns an error.
func setField(fieldVal reflect.Value, value string, meta fieldMeta, fieldName string) error {
	ft := fieldVal.Type()

	// []byte is a special case — treat as raw bytes, not a slice of uint8.
	if ft == byteSliceType {
		fieldVal.SetBytes([]byte(value))

		return nil
	}

	// Slice of T (excluding []byte handled above).
	if ft.Kind() == reflect.Slice {
		return setSliceField(fieldVal, value, meta, fieldName)
	}

	converted, err := convertScalar(ft, value)
	if err != nil {
		return conversionError(meta, fieldName, value, ft, err)
	}

	fieldVal.Set(converted)

	return nil
}

// setSliceField splits value by the configured separator and converts each
// element to the slice's element type.
func setSliceField(fieldVal reflect.Value, value string, meta fieldMeta, fieldName string) error {
	sep := meta.sep
	if sep == "" {
		sep = ","
	}

	parts := strings.Split(value, sep)
	elemType := fieldVal.Type().Elem()
	slice := reflect.MakeSlice(fieldVal.Type(), 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		converted, err := convertScalar(elemType, part)
		if err != nil {
			return conversionError(meta, fieldName, part, elemType, err)
		}

		slice = reflect.Append(slice, converted)
	}

	fieldVal.Set(slice)

	return nil
}

// convertScalar converts a string to the given reflect.Type.
// Supported types: string, bool, int*, uint*, float*, time.Duration.
//
//nolint:cyclop
func convertScalar(t reflect.Type, value string) (reflect.Value, error) {
	// time.Duration is a named int64 — handle before the int64 case.
	if t == durationType {
		d, err := time.ParseDuration(value)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("parse duration %q: %w", value, err)
		}

		return reflect.ValueOf(d), nil
	}

	//nolint:exhaustive // only relevant kinds are handled; default returns error
	switch t.Kind() {
	case reflect.String:
		return reflect.ValueOf(value).Convert(t), nil

	case reflect.Bool:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("parse bool %q: %w", value, err)
		}

		return reflect.ValueOf(b).Convert(t), nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(value, 10, t.Bits())
		if err != nil {
			return reflect.Value{}, fmt.Errorf("parse int %q: %w", value, err)
		}

		v := reflect.New(t).Elem()
		v.SetInt(n)

		return v, nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(value, 10, t.Bits())
		if err != nil {
			return reflect.Value{}, fmt.Errorf("parse uint %q: %w", value, err)
		}

		v := reflect.New(t).Elem()
		v.SetUint(n)

		return v, nil

	case reflect.Float32, reflect.Float64:
		n, err := strconv.ParseFloat(value, t.Bits())
		if err != nil {
			return reflect.Value{}, fmt.Errorf("parse float %q: %w", value, err)
		}

		v := reflect.New(t).Elem()
		v.SetFloat(n)

		return v, nil

	default:
		return reflect.Value{}, fmt.Errorf("unsupported type %s", t)
	}
}

// conversionError builds and either returns or panics with a conversion error.
func conversionError(meta fieldMeta, fieldName, value string, t reflect.Type, cause error) error {
	msg := meta.errMsg
	if msg == "" {
		msg = fmt.Sprintf("field %q: cannot convert %q to %s: %v", fieldName, value, t, cause)
	}

	if meta.panic {
		panic("duck/config: " + msg)
	}

	return fmt.Errorf("%w: %s", ErrConversion, msg)
}
