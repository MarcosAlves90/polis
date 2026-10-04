package spec

import (
	"encoding/json"
	"testing"
)

func TestWorkspaceStatusSchemaIsEmbeddedAndClosed(t *testing.T) {
	var raw []byte
	for _, resource := range OfflineResources() {
		if resource.Path == "schemas/workspace-status-v1.schema.json" {
			raw = resource.Data
			break
		}
	}
	if len(raw) == 0 {
		t.Fatal("workspace status schema is not embedded in the offline kit")
	}
	var schema struct {
		Type                 string                    `json:"type"`
		AdditionalProperties bool                      `json:"additionalProperties"`
		Required             []string                  `json:"required"`
		Properties           map[string]map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("decode workspace status schema: %v", err)
	}
	if schema.Type != "object" || schema.AdditionalProperties {
		t.Fatalf("schema must be a closed JSON object: type=%q additionalProperties=%t", schema.Type, schema.AdditionalProperties)
	}
	for _, field := range []string{
		"schema_version", "status", "checkpoint_state", "recorded_validation_status",
		"report_authenticated", "current_validation_established", "proof_input_digests_bound",
		"delivery_artifact_verified", "locked_base_commit", "recorded_base_commit", "recorded_target_tree", "current_target_tree",
		"recorded_contract_sha256", "contract_sha256", "recorded_policy_sha256", "policy_sha256", "differences", "next_action",
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
	for _, field := range []string{"report_authenticated", "current_validation_established", "proof_input_digests_bound", "delivery_artifact_verified"} {
		if schema.Properties[field]["const"] != false {
			t.Errorf("status schema must deny authority claim %q", field)
		}
	}
	if schema.Properties["next_action"]["const"] != "polis workspace validate" {
		t.Fatal("status schema must direct fresh validation to polis workspace validate")
	}
}
