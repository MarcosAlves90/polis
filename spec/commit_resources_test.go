package spec

import "testing"

func TestOfflineResourcesIncludeCommitIntentSchemas(t *testing.T) {
	want := map[string]bool{
		"schemas/change-contract-v5.schema.json": false,
		"schemas/change-contract-v6.schema.json": false,
	}
	for _, resource := range OfflineResources() {
		if _, ok := want[resource.Path]; ok {
			want[resource.Path] = len(resource.Data) != 0
		}
	}
	for name, present := range want {
		if !present {
			t.Errorf("offline resources omit %s", name)
		}
	}
}
