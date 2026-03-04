// Package config provides a generic configuration loader that populates
// user-defined structs from environment variables and config files.
//
// Fields are controlled via struct tags:
//
//	type AppConfig struct {
//	    Port    string   `env:"PORT"    duck:"default=8080"`
//	    DBUrl   string   `env:"DB_URL"  duck:"mandatory,panic"`
//	    Tags    []string `env:"TAGS"    duck:"sep=,"`
//	    AppName string   `env:"APP_NAME" duck:"mandatory,errMsg=APP_NAME is required"`
//	}
//
// Source priority (highest to lowest):
//  1. Environment variables (via the `env` tag)
//  2. Config file (JSON or YAML, auto-detected by extension)
//  3. Default values (via duck:"default=...")
//
// The sep= directive controls how env var string values are split into slice
// elements. It has no effect on JSON/YAML files, which use native array syntax.
package config

import (
	"errors"
)

// ErrMissingMandatory is returned when a mandatory field has no value and
// does not have the panic tag.
var ErrMissingMandatory = errors.New("missing mandatory field")

// ErrConversion is returned when a string value cannot be converted to the
// target field type and the field does not have the panic tag.
var ErrConversion = errors.New("conversion error")

// source represents a configuration source.
type source int

const (
	sourceEnv  source = iota // environment variables
	sourceFile               // config file (JSON or YAML)
)

// options holds the loader configuration.
type options struct {
	sources  []source
	filePath string
}

// Option is a functional option for configuring the config loader.
type Option func(*options)

// WithEnv instructs the loader to read values from environment variables.
// Each field must have an `env:"VAR_NAME"` struct tag to be populated.
func WithEnv() Option {
	return func(o *options) {
		o.sources = append(o.sources, sourceEnv)
	}
}

// WithFile instructs the loader to read values from a config file.
// The format is auto-detected from the file extension (.json, .yaml, .yml).
// JSON and YAML arrays are mapped to Go slice fields directly, without
// requiring the sep= tag.
func WithFile(path string) Option {
	return func(o *options) {
		o.sources = append(o.sources, sourceFile)
		o.filePath = path
	}
}
