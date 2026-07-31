package provider

import (
	"strconv"
	"strings"
)

var numericSchemaFields = map[string]struct{}{
	"minimum": {}, "maximum": {}, "exclusiveMinimum": {}, "exclusiveMaximum": {},
	"minLength": {}, "maxLength": {}, "minItems": {}, "maxItems": {},
	"minProperties": {}, "maxProperties": {}, "multipleOf": {},
}

var arraySchemaKeys = map[string]struct{}{
	"enum": {}, "required": {}, "anyOf": {}, "oneOf": {}, "allOf": {}, "prefixItems": {},
}

var schemaArrayOfSchemas = map[string]struct{}{
	"anyOf": {}, "oneOf": {}, "allOf": {}, "prefixItems": {},
}

var schemaSlotKeys = map[string]struct{}{
	"items": {}, "additionalProperties": {}, "propertyNames": {}, "contains": {},
	"not": {}, "if": {}, "then": {}, "else": {},
	"unevaluatedProperties": {}, "additionalItems": {},
}

func isSchemaPlaceholder(v interface{}) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]")
}

func coerceNumericString(v interface{}) interface{} {
	s, ok := v.(string)
	if !ok {
		return v
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return v
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return v
}

func coerceIndexedObjectToArray(v interface{}) ([]interface{}, bool) {
	if a, ok := v.([]interface{}); ok {
		return a, true
	}
	m, ok := v.(map[string]interface{})
	if !ok || len(m) == 0 {
		return nil, false
	}
	out := make([]interface{}, len(m))
	for k, val := range m {
		idx, err := strconv.Atoi(k)
		if err != nil || idx < 0 || idx >= len(m) {
			return nil, false
		}
		out[idx] = val
	}
	// Verify contiguous 0..n-1
	for i := range out {
		if _, has := m[strconv.Itoa(i)]; !has {
			return nil, false
		}
	}
	return out, true
}

func stripInvalidSchemaConstructs(schema interface{}) interface{} {
	switch s := schema.(type) {
	case []interface{}:
		out := make([]interface{}, len(s))
		for i, e := range s {
			out[i] = stripInvalidSchemaConstructs(e)
		}
		return out
	case map[string]interface{}:
		result := map[string]interface{}{}
		for key, value := range s {
			if _, ok := numericSchemaFields[key]; ok {
				result[key] = coerceNumericString(value)
				continue
			}
			if _, ok := arraySchemaKeys[key]; ok {
				arr, ok := coerceIndexedObjectToArray(value)
				if !ok {
					continue
				}
				if _, nest := schemaArrayOfSchemas[key]; nest {
					mapped := make([]interface{}, len(arr))
					for i, e := range arr {
						mapped[i] = stripInvalidSchemaConstructs(e)
					}
					result[key] = mapped
				} else {
					result[key] = arr
				}
				continue
			}
			if _, ok := schemaSlotKeys[key]; ok {
				switch value.(type) {
				case map[string]interface{}, []interface{}:
					result[key] = stripInvalidSchemaConstructs(value)
				case bool:
					result[key] = value
				default:
					if isSchemaPlaceholder(value) {
						result[key] = map[string]interface{}{}
					} else {
						result[key] = value
					}
				}
				continue
			}
			if key == "const" {
				if isSchemaPlaceholder(value) {
					continue
				}
				result[key] = value
				continue
			}
			if key == "properties" {
				if props, ok := value.(map[string]interface{}); ok {
					out := map[string]interface{}{}
					for pn, ps := range props {
						switch ps.(type) {
						case map[string]interface{}, []interface{}:
							out[pn] = stripInvalidSchemaConstructs(ps)
						case bool:
							out[pn] = ps
						default:
							if isSchemaPlaceholder(ps) {
								out[pn] = map[string]interface{}{}
							} else {
								out[pn] = ps
							}
						}
					}
					result[key] = out
				}
				continue
			}
			if key == "$defs" || key == "definitions" || key == "patternProperties" || key == "dependentSchemas" {
				if defs, ok := value.(map[string]interface{}); ok {
					out := map[string]interface{}{}
					for dn, ds := range defs {
						out[dn] = stripInvalidSchemaConstructs(ds)
					}
					result[key] = out
				}
				continue
			}
			switch value.(type) {
			case map[string]interface{}, []interface{}:
				result[key] = stripInvalidSchemaConstructs(value)
			default:
				result[key] = value
			}
		}
		return result
	default:
		if isSchemaPlaceholder(schema) {
			return map[string]interface{}{}
		}
		return schema
	}
}

func sanitizeClaudeToolSchemas(body map[string]interface{}) {
	tools, ok := body["tools"].([]interface{})
	if !ok {
		return
	}
	out := make([]interface{}, len(tools))
	for i, raw := range tools {
		tool, ok := raw.(map[string]interface{})
		if !ok {
			out[i] = raw
			continue
		}
		if _, has := tool["input_schema"]; !has {
			out[i] = tool
			continue
		}
		cp := cloneMap(tool)
		cp["input_schema"] = stripInvalidSchemaConstructs(tool["input_schema"])
		out[i] = cp
	}
	body["tools"] = out
}
