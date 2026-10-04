package spec

import (
	"encoding/json"
	"testing"
)

func TestWorkspaceValidationReportSchemaIsEmbeddedAndClosed(t *testing.T) {
	var raw []byte
	for _, resource := range OfflineResources() {
		if resource.Path == "schemas/workspace-validation-v1.schema.json" {
			raw = resource.Data
			break
		}
	}
	if len(raw) == 0 {
		t.Fatal("workspace validation report schema is not embedded in the offline kit")
	}
	var schema struct {
		Type                 string                    `json:"type"`
		AdditionalProperties bool                      `json:"additionalProperties"`
		Required             []string                  `json:"required"`
		Properties           map[string]map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("decode workspace validation schema: %v", err)
	}
	if schema.Type != "object" || schema.AdditionalProperties {
		t.Fatalf("schema must be a closed JSON object: type=%q additionalProperties=%t", schema.Type, schema.AdditionalProperties)
	}
	for _, field := range []string{
		"schema_version", "status", "validation_kind", "workspace_validated",
		"delivery_artifact_built", "delivery_artifact_verified", "base_commit",
		"target_tree", "contract_sha256", "policy_sha256", "validation_level",
		"enabled_gates", "disabled_gates", "producer_gate_statuses",
	} {
		if _, ok := schema.Properties[field]; !ok {
			t.Errorf("schema lacks property %q", field)
		}
		found := false
		for _, required := range schema.Required {
			if required == field {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("schema does not require property %q", field)
		}
	}
	if schema.Properties["delivery_artifact_built"]["const"] != false || schema.Properties["delivery_artifact_verified"]["const"] != false {
		t.Fatal("workspace reports must explicitly deny delivery artifact claims")
	}
}
