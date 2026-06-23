package datago

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSchemaFilesAreValidJSON(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", "schemas"))
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		found[entry.Name()] = true
		data, err := os.ReadFile(filepath.Join(root, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatalf("%s is not valid JSON: %v", entry.Name(), err)
		}
		if payload["$schema"] == "" || payload["$id"] == "" || payload["title"] == "" {
			t.Fatalf("%s is missing schema metadata: %#v", entry.Name(), payload)
		}
	}
	for _, name := range []string{
		"datapan.specs.v1.schema.json",
		"datapan.providers.v1.schema.json",
		"datapan.verification.v1.schema.json",
	} {
		if !found[name] {
			t.Fatalf("missing schema file %s", name)
		}
	}
}

func TestRegistryReleaseDocReferencesArtifacts(t *testing.T) {
	path := filepath.Clean(filepath.Join("..", "..", "docs", "registry-release.md"))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"schemas/datapan.specs.v1.schema.json",
		"schemas/datapan.providers.v1.schema.json",
		"schemas/datapan.verification.v1.schema.json",
		"data/data-go-kr.registry.json",
		"reports/provider-backlog.json",
		"reports/latest-verification.json",
		"datapan catalog release draft",
		"datapan catalog update data-go-kr",
		"datapan catalog providers",
		"datapan catalog verify",
		"datapan catalog verify --input",
		"datapan catalog audit",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("registry release doc should reference %q", want)
		}
	}
}

func TestProviderAdaptersDocReferencesContracts(t *testing.T) {
	path := filepath.Clean(filepath.Join("..", "..", "docs", "provider-adapters.md"))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"internal/provider",
		"internal/provider.Registry",
		"type Adapter interface",
		"datapan catalog providers",
		"--status missing",
		"--status adapter",
		"--kind external_endpoint",
		"--provider q-net",
		"openapi.q-net.or.kr",
		"qnet_adapter_observation_required",
		"Adapter Readiness Bar",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("provider adapters doc should reference %q", want)
		}
	}
}
