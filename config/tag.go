package config

import (
	"strings"
)

// fieldMeta holds the parsed duck tag metadata for a single struct field.
type fieldMeta struct {
	required   bool
	panic      bool
	hasDefault bool
	defaultV   string
	errMsg     string
	sep        string // slice element separator, defaults to ","
}

// parseDuckTag parses a `duck:"..."` struct tag value into a fieldMeta.
//
// Supported directives:
//   - mandatory        — field must have a non-empty value
//   - panic            — panic instead of returning an error when the field is invalid
//   - default=<value>  — fallback value when the field is not set; may contain commas
//   - errMsg=<message> — custom error/panic message; may contain commas
//   - sep=<char>       — separator for slice fields (default: ",")
//
// Parsing rules:
//   - Directives are comma-separated.
//   - Once a "default=" or "errMsg=" directive is encountered, the rest of the
//     tag (including any commas) is consumed as the value of that directive.
//     This means "default=" and "errMsg=" must be the last directive in the tag.
//
// Example:
//
//	`duck:"mandatory,sep=:,default=a:b:c"`   — valid, default is "a:b:c"
//	`duck:"mandatory,default=a,b,c"`         — valid, default is "a,b,c"
//	`duck:"default=a,b,c,mandatory"`         — INVALID: mandatory is consumed as part of default
//
//nolint:cyclop // directive parser — each case is a distinct directive, complexity is structural not accidental
func parseDuckTag(tag string) fieldMeta {
	var meta fieldMeta

	if tag == "" {
		return meta
	}

	remaining := tag

	for remaining != "" {
		// Find the next directive boundary.
		idx := strings.Index(remaining, ",")

		var directive string

		if idx == -1 {
			directive = remaining
			remaining = ""
		} else {
			directive = remaining[:idx]
			remaining = remaining[idx+1:]
		}

		directive = strings.TrimSpace(directive)

		switch {
		case directive == "required":
			meta.required = true

		case directive == "panic":
			meta.panic = true

		case strings.HasPrefix(directive, "sep="):
			meta.sep = strings.TrimPrefix(directive, "sep=")

		case strings.HasPrefix(directive, "default="):
			// Consume the rest of the tag as the default value — it may contain commas.
			value := strings.TrimPrefix(directive, "default=")
			if remaining != "" {
				value = value + "," + remaining
				remaining = ""
			}

			meta.defaultV = value
			meta.hasDefault = true

		case strings.HasPrefix(directive, "errMsg="):
			// Same as default= — consume the rest.
			value := strings.TrimPrefix(directive, "errMsg=")
			if remaining != "" {
				value = value + "," + remaining
				remaining = ""
			}

			meta.errMsg = value
		}
	}

	return meta
}
