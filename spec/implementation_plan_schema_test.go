package spec

import (
	"encoding/json"
	"regexp"
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

func TestImplementationPlanSchemaStepIDPatternsMatchRuntime(t *testing.T) {
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
	var schema struct {
		Definitions struct {
			Step struct {
				Properties map[string]struct {
					Pattern string `json:"pattern"`
					Items   struct {
						Pattern string `json:"pattern"`
					} `json:"items"`
				} `json:"properties"`
			} `json:"step"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("parse implementation plan schema: %v", err)
	}
	idPattern := schema.Definitions.Step.Properties["id"].Pattern
	dependencyPattern := schema.Definitions.Step.Properties["depends_on"].Items.Pattern
	wantPattern := canonicalPlanStepIDPattern.String()
	if idPattern != wantPattern || dependencyPattern != wantPattern {
		t.Fatalf("schema plan ID patterns differ from runtime: id=%q depends_on=%q runtime=%q", idPattern, dependencyPattern, wantPattern)
	}
	idRegexp, err := regexp.Compile(idPattern)
	if err != nil {
		t.Fatalf("compile schema plan ID pattern: %v", err)
	}
	dependencyRegexp, err := regexp.Compile(dependencyPattern)
	if err != nil {
		t.Fatalf("compile schema dependency ID pattern: %v", err)
	}
	for _, test := range []struct {
		id        string
		wantValid bool
	}{
		{id: "PLAN-001", wantValid: true},
		{id: "PLAN-999", wantValid: true},
		{id: "PLAN-1000", wantValid: true},
		{id: "PLAN-18446744073709551616", wantValid: true},
		{id: "PLAN-000"},
		{id: "PLAN-0001"},
	} {
		if got := idRegexp.MatchString(test.id); got != test.wantValid {
			t.Errorf("schema id pattern match for %q = %t, want %t", test.id, got, test.wantValid)
		}
		if got := dependencyRegexp.MatchString(test.id); got != test.wantValid {
			t.Errorf("schema dependency pattern match for %q = %t, want %t", test.id, got, test.wantValid)
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
