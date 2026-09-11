package spec

import "embed"

// OfflineResource is a canonical resource included in the POLIS V6 offline kit.
type OfflineResource struct {
	Path string
	Data []byte
}

// The offline kit embeds only the resources needed to operate and describe the
// current V6 protocol. The source files remain the canonical copies.
//
//go:embed POLIS-OFFLINE.md POLIS-SPEC-v6.md schemas/policy.schema.json schemas/change-contract.schema.json schemas/change-contract-v3.schema.json schemas/change-contract-v4.schema.json schemas/manifest.schema.json schemas/evidence-event.schema.json schemas/signature.schema.json
var offlineResources embed.FS

var offlineResourcePaths = []string{
	"POLIS-OFFLINE.md",
	"POLIS-SPEC-v6.md",
	"schemas/change-contract.schema.json",
	"schemas/change-contract-v3.schema.json",
	"schemas/change-contract-v4.schema.json",
	"schemas/evidence-event.schema.json",
	"schemas/manifest.schema.json",
	"schemas/policy.schema.json",
	"schemas/signature.schema.json",
}

// OfflineResources returns copies so callers cannot mutate the embedded source.
func OfflineResources() []OfflineResource {
	resources := make([]OfflineResource, 0, len(offlineResourcePaths))
	for _, resourcePath := range offlineResourcePaths {
		data, err := offlineResources.ReadFile(resourcePath)
		if err != nil {
			panic("embedded POLIS offline resource is missing: " + resourcePath)
		}
		resources = append(resources, OfflineResource{Path: resourcePath, Data: append([]byte(nil), data...)})
	}
	return resources
}
