package provider

import "encoding/json"

// CleanSchema strips provider-incompatible JSON Schema keywords.
//
// Modes:
//   - "gemini": strip $ref, $defs, additionalProperties, examples, default
//   - "anthropic": strip $ref, $defs
//   - "" (empty): pass-through, no changes
func CleanSchema(schema json.RawMessage, mode string) json.RawMessage {
	if mode == "" || len(schema) == 0 {
		return schema
	}

	var obj map[string]any
	if err := json.Unmarshal(schema, &obj); err != nil {
		return schema
	}

	var strip []string
	switch mode {
	case "gemini":
		strip = []string{"$ref", "$defs", "additionalProperties", "examples", "default"}
	case "anthropic":
		strip = []string{"$ref", "$defs"}
	default:
		return schema
	}

	cleanObject(obj, strip)

	out, err := json.Marshal(obj)
	if err != nil {
		return schema
	}
	return out
}

func cleanObject(obj map[string]any, strip []string) {
	for _, key := range strip {
		delete(obj, key)
	}

	// Recurse into "properties".
	if props, ok := obj["properties"].(map[string]any); ok {
		for _, v := range props {
			if child, ok := v.(map[string]any); ok {
				cleanObject(child, strip)
			}
		}
	}

	// Recurse into "items" (array schemas).
	if items, ok := obj["items"].(map[string]any); ok {
		cleanObject(items, strip)
	}

	// Recurse into anyOf/oneOf/allOf.
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		if arr, ok := obj[key].([]any); ok {
			for _, item := range arr {
				if child, ok := item.(map[string]any); ok {
					cleanObject(child, strip)
				}
			}
		}
	}
}
