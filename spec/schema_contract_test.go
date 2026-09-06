package spec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestChangeContractJSONSchemasCoverSupportedVersions(t *testing.T) {
	root := "schemas"
	for _, name := range []string{"change-contract-v1.schema.json", "change-contract-v2.schema.json", "change-contract-v3.schema.json", "change-contract-v4.schema.json"} {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(root, "change-contract.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var aggregate struct {
		OneOf []struct {
			Ref string `json:"$ref"`
		} `json:"oneOf"`
	}
	if err := json.Unmarshal(raw, &aggregate); err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(aggregate.OneOf))
	for _, item := range aggregate.OneOf {
		got = append(got, item.Ref)
	}
	want := []string{"change-contract-v1.schema.json", "change-contract-v2.schema.json", "change-contract-v3.schema.json", "change-contract-v4.schema.json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("aggregate refs=%v want=%v", got, want)
	}
}
