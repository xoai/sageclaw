package provider

import (
	"encoding/json"
	"testing"
)

func TestCleanSchema_Gemini(t *testing.T) {
	schema := json.RawMessage(`{
		"type": "object",
		"$ref": "#/$defs/Foo",
		"$defs": {"Foo": {"type": "string"}},
		"additionalProperties": false,
		"examples": [{"a": 1}],
		"default": "test",
		"properties": {
			"name": {
				"type": "string",
				"default": "hello",
				"examples": ["a"]
			},
			"items": {
				"type": "array",
				"items": {
					"type": "object",
					"$ref": "#/$defs/Item",
					"additionalProperties": true
				}
			}
		}
	}`)

	cleaned := CleanSchema(schema, "gemini")

	var obj map[string]any
	if err := json.Unmarshal(cleaned, &obj); err != nil {
		t.Fatalf("failed to unmarshal cleaned schema: %v", err)
	}

	// Top-level stripped keys.
	for _, key := range []string{"$ref", "$defs", "additionalProperties", "examples", "default"} {
		if _, ok := obj[key]; ok {
			t.Errorf("expected %q to be stripped at top level", key)
		}
	}

	// "type" preserved.
	if obj["type"] != "object" {
		t.Errorf("expected type=object, got %v", obj["type"])
	}

	// Nested property "name" should have default/examples stripped.
	props := obj["properties"].(map[string]any)
	nameProp := props["name"].(map[string]any)
	if _, ok := nameProp["default"]; ok {
		t.Error("expected default stripped from name property")
	}
	if _, ok := nameProp["examples"]; ok {
		t.Error("expected examples stripped from name property")
	}

	// Nested items schema should have $ref and additionalProperties stripped.
	itemsProp := props["items"].(map[string]any)
	itemsItems := itemsProp["items"].(map[string]any)
	if _, ok := itemsItems["$ref"]; ok {
		t.Error("expected $ref stripped from nested items")
	}
	if _, ok := itemsItems["additionalProperties"]; ok {
		t.Error("expected additionalProperties stripped from nested items")
	}
}

func TestCleanSchema_Anthropic(t *testing.T) {
	schema := json.RawMessage(`{
		"type": "object",
		"$ref": "#/$defs/Foo",
		"$defs": {"Foo": {"type": "string"}},
		"additionalProperties": false,
		"default": "test"
	}`)

	cleaned := CleanSchema(schema, "anthropic")

	var obj map[string]any
	json.Unmarshal(cleaned, &obj)

	// $ref and $defs stripped.
	if _, ok := obj["$ref"]; ok {
		t.Error("expected $ref stripped for anthropic")
	}
	if _, ok := obj["$defs"]; ok {
		t.Error("expected $defs stripped for anthropic")
	}
	// additionalProperties and default preserved for anthropic.
	if _, ok := obj["additionalProperties"]; !ok {
		t.Error("expected additionalProperties preserved for anthropic")
	}
	if _, ok := obj["default"]; !ok {
		t.Error("expected default preserved for anthropic")
	}
}

func TestCleanSchema_Passthrough(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","$ref":"#/foo"}`)
	cleaned := CleanSchema(schema, "")

	if string(cleaned) != string(schema) {
		t.Errorf("expected passthrough, got %s", cleaned)
	}
}

func TestCleanSchema_InvalidJSON(t *testing.T) {
	schema := json.RawMessage(`not json`)
	cleaned := CleanSchema(schema, "gemini")

	if string(cleaned) != string(schema) {
		t.Error("expected invalid JSON to pass through unchanged")
	}
}
