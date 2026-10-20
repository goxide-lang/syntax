package proc_macro

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/goxide-lang/syntax/token"
)

// A Group contributes both an object and a children array to JSON nesting.
// Token semantics retain MaxDepth; the framing allowance accommodates wrappers.
const maxJSONDepth = 2*token.MaxDepth + 16

type jsonValidator struct {
	decoder *json.Decoder
	fields  map[reflect.Type]map[string]reflect.Type
}

var wireTokenType = reflect.TypeFor[token.WireToken]()

func validateJSON(data []byte, shape any) error {
	if !utf8.Valid(data) {
		return fmt.Errorf("protocol JSON is not valid UTF-8")
	}
	if err := validateUnicodeEscapes(data); err != nil {
		return err
	}
	typ := reflect.TypeOf(shape)
	if typ == nil {
		return fmt.Errorf("missing JSON schema")
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	validator := jsonValidator{decoder: decoder, fields: make(map[reflect.Type]map[string]reflect.Type)}
	if _, err := validator.value(typ, 0); err != nil {
		return fmt.Errorf("invalid protocol JSON: %w", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("protocol JSON has trailing content")
		}
		return fmt.Errorf("protocol JSON has trailing content: %w", err)
	}
	return nil
}

func (v *jsonValidator) value(typ reflect.Type, depth int) (any, error) {
	if depth > maxJSONDepth {
		return nil, fmt.Errorf("JSON nesting exceeds %d levels", maxJSONDepth)
	}
	item, err := v.decoder.Token()
	if err != nil {
		return nil, err
	}
	if item == nil {
		switch typ.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Map:
			return nil, nil
		default:
			return nil, fmt.Errorf("null is not valid for %s", typ)
		}
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch typ.Kind() {
	case reflect.Struct:
		if item != json.Delim('{') {
			return nil, fmt.Errorf("expected object for %s", typ)
		}
		fields := v.structFields(typ)
		present := make(map[string]any)
		for v.decoder.More() {
			keyToken, err := v.decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, fmt.Errorf("expected object key")
			}
			if _, exists := present[key]; exists {
				return nil, fmt.Errorf("duplicate field %q", key)
			}
			fieldType, exists := fields[key]
			if !exists {
				return nil, fmt.Errorf("unknown field %q", key)
			}
			value, err := v.value(fieldType, depth+1)
			if err != nil {
				return nil, fmt.Errorf("field %q: %w", key, err)
			}
			present[key] = value
		}
		if err := v.close('}'); err != nil {
			return nil, err
		}
		if typ == wireTokenType {
			if err := validateWireFields(present); err != nil {
				return nil, err
			}
		}
	case reflect.Map:
		if item != json.Delim('{') || typ.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("expected string-keyed object for %s", typ)
		}
		keys := make(map[string]bool)
		for v.decoder.More() {
			keyToken, err := v.decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, fmt.Errorf("expected object key")
			}
			if keys[key] {
				return nil, fmt.Errorf("duplicate key %q", key)
			}
			if len(keys) >= token.MaxNodes {
				return nil, fmt.Errorf("object exceeds %d entries", token.MaxNodes)
			}
			keys[key] = true
			if _, err := v.value(typ.Elem(), depth+1); err != nil {
				return nil, err
			}
		}
		return nil, v.close('}')
	case reflect.Slice:
		if item != json.Delim('[') {
			return nil, fmt.Errorf("expected array for %s", typ)
		}
		for count := 0; v.decoder.More(); count++ {
			if count >= token.MaxNodes {
				return nil, fmt.Errorf("array exceeds %d entries", token.MaxNodes)
			}
			if _, err := v.value(typ.Elem(), depth+1); err != nil {
				return nil, err
			}
		}
		return nil, v.close(']')
	case reflect.String:
		if _, ok := item.(string); !ok {
			return nil, fmt.Errorf("expected string for %s", typ)
		}
		return item, nil
	case reflect.Bool:
		if _, ok := item.(bool); !ok {
			return nil, fmt.Errorf("expected boolean for %s", typ)
		}
		return item, nil
	case reflect.Uint32:
		number, ok := item.(json.Number)
		if !ok {
			return nil, fmt.Errorf("expected integer for %s", typ)
		}
		value, err := strconv.ParseUint(string(number), 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid uint32 %q", number)
		}
		return uint32(value), nil
	default:
		return nil, fmt.Errorf("unsupported JSON schema type %s", typ)
	}
	return nil, nil
}

func (v *jsonValidator) close(want byte) error {
	item, err := v.decoder.Token()
	if err != nil {
		return err
	}
	if item != json.Delim(want) {
		return fmt.Errorf("expected %c", want)
	}
	return nil
}

func (v *jsonValidator) structFields(typ reflect.Type) map[string]reflect.Type {
	if fields, ok := v.fields[typ]; ok {
		return fields
	}
	fields := make(map[string]reflect.Type)
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name != "" && name != "-" {
			fields[name] = field.Type
		}
	}
	v.fields[typ] = fields
	return fields
}

func validateWireFields(fields map[string]any) error {
	if ref, exists := fields["ref"]; exists {
		if ref != uint32(0) {
			for field := range fields {
				if field != "ref" && field != "span" && field != "open" && field != "close" {
					return fmt.Errorf("input reference cannot carry %q", field)
				}
			}
			return nil
		}
		return fmt.Errorf("input reference must be nonzero")
	}
	kind, ok := fields["kind"].(string)
	if !ok {
		return fmt.Errorf("new token needs a kind")
	}
	allowed := " kind context span "
	switch token.Kind(kind) {
	case token.IdentKind:
		allowed += "text raw "
	case token.KeywordKind, token.PunctKind:
		allowed += "text "
	case token.LiteralKind:
		allowed += "text literal "
	case token.SemicolonKind:
		allowed += "implicit "
	case token.GroupKind:
		allowed += "delimiter children open close "
	case token.FragmentKind:
		allowed += "fragment children "
	default:
		return fmt.Errorf("unknown token kind %q", kind)
	}
	for field := range fields {
		if !strings.Contains(allowed, " "+field+" ") {
			return fmt.Errorf("%s token cannot carry %q", kind, field)
		}
	}
	return nil
}

// encoding/json replaces unpaired UTF-16 escape surrogates with U+FFFD.
// Reject them before decoding instead of silently accepting changed names.
func validateUnicodeEscapes(data []byte) error {
	inString := false
	for i := 0; i < len(data); i++ {
		switch data[i] {
		case '"':
			inString = !inString
		case '\\':
			if !inString || i+1 >= len(data) {
				continue
			}
			i++
			if data[i] != 'u' {
				continue
			}
			if i+4 >= len(data) {
				return fmt.Errorf("truncated Unicode escape")
			}
			value, err := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
			if err != nil {
				return fmt.Errorf("invalid Unicode escape")
			}
			i += 4
			if value >= 0xdc00 && value <= 0xdfff {
				return fmt.Errorf("unpaired low Unicode surrogate")
			}
			if value < 0xd800 || value > 0xdbff {
				continue
			}
			if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
				return fmt.Errorf("unpaired high Unicode surrogate")
			}
			low, err := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return fmt.Errorf("unpaired high Unicode surrogate")
			}
			i += 6
		}
	}
	return nil
}
