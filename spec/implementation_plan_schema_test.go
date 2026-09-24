package spec

import (
	"encoding/json"
	"testing"
)

func TestImplementationPlanSchemaIsEmbeddedAndClosed(t *testing.T) {
	var raw []byte
	for _, resource := range OfflineResources() {
		if resource.Path == "schemas/implementation-plan.schema.json" {
			raw = resource.Data
			break
		}
	}
	if len(raw) == 0 {
		t.Fatal("implementation plan schema is missing from offline resources")
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("parse implementation plan schema: %v", err)
	}
	if schema["title"] != "POLIS Implementation Plan v1" || schema["additionalProperties"] != false {
		t.Fatalf("unexpected implementation plan schema envelope: title=%v additionalProperties=%v", schema["title"], schema["additionalProperties"])
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("implementation plan schema properties are missing")
	}
	steps, ok := properties["steps"].(map[string]any)
	if !ok {
		t.Fatal("implementation plan schema steps property is missing")
	}
	items, ok := steps["items"].(map[string]any)
	if !ok || items["$ref"] != "#/$defs/step" {
		t.Fatal("implementation plan step schema must reject unknown fields")
	}
	definitions, ok := schema["$defs"].(map[string]any)
	if !ok {
		t.Fatal("implementation plan schema definitions are missing")
	}
	step, ok := definitions["step"].(map[string]any)
	if !ok || step["additionalProperties"] != false {
		t.Fatal("implementation plan step schema must reject unknown fields")
	}
	for _, field := range []string{"schema_version", "change_contract_sha256", "git_object_format", "base_commit", "base_tree", "strategy", "steps"} {
		if !containsPlanValue(anyStrings(schema["required"]), field) {
			t.Errorf("implementation plan schema does not require %q", field)
		}
	}
}

func TestManifestV6SchemaRequiresPlanDigest(t *testing.T) {
	for _, resource := range OfflineResources() {
		if resource.Path != "schemas/manifest-v6.schema.json" {
			continue
		}
		var schema struct {
			Title    string         `json:"title"`
			Required []string       `json:"required"`
			Props    map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(resource.Data, &schema); err != nil {
			t.Fatal(err)
		}
		version, ok := schema.Props["format_version"].(map[string]any)
		if !ok || version["const"] != float64(ImplementationPlanFormatVersion) {
			t.Fatalf("format-v6 manifest schema has wrong version constraint: %v", schema.Props["format_version"])
		}
		if schema.Title != "POLIS Manifest format v6" || !containsPlanValue(schema.Required, "implementation_plan_sha256") {
			t.Fatalf("format-v6 manifest schema does not require the plan digest: %+v", schema)
		}
		return
	}
	t.Fatal("format-v6 manifest schema is missing from offline resources")
}

func anyStrings(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	strings := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			strings = append(strings, text)
		}
	}
	return strings
}
