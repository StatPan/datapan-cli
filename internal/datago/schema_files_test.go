package datago

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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
		if !schemaFilePattern.MatchString(entry.Name()) {
			t.Fatalf("%s does not follow datapan.<contract>.vN.schema.json", entry.Name())
		}
		wantID := "https://schemas.datapan.dev/" + entry.Name()
		if payload["$id"] != wantID {
			t.Fatalf("%s has $id %q, want %q", entry.Name(), payload["$id"], wantID)
		}
	}
	for _, name := range []string{
		"datapan.specs.v1.schema.json",
		"datapan.dependencies.v1.schema.json",
		"datapan.adapter-targets.v1.schema.json",
		"datapan.route-disposition.v1.schema.json",
		"datapan.providers.v1.schema.json",
		"datapan.coverage.v1.schema.json",
		"datapan.verification.v1.schema.json",
		"datapan.verification-plan.v1.schema.json",
		"datapan.verification-summary.v1.schema.json",
		"datapan.release-manifest.v1.schema.json",
		"datapan.release-verification.v1.schema.json",
		"datapan.release-readiness.v1.schema.json",
		"datapan.schema-index.v1.schema.json",
		"datapan.catalog-diff.v1.schema.json",
		"datapan.error-catalog.v1.schema.json",
		"datapan.catalog-audit.v1.schema.json",
		"datapan.provider-index.v1.schema.json",
		"datapan.studio-datasets.v1.schema.json",
		"datapan.studio-bundle.v1.schema.json",
	} {
		if !found[name] {
			t.Fatalf("missing schema file %s", name)
		}
	}
}

var schemaFilePattern = regexp.MustCompile(`^datapan\.[a-z0-9-]+\.v[0-9]+\.schema\.json$`)

func TestRegistryReleaseDocReferencesArtifacts(t *testing.T) {
	path := filepath.Clean(filepath.Join("..", "..", "docs", "registry-release.md"))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"schemas/datapan.specs.v1.schema.json",
		"schemas/datapan.dependencies.v1.schema.json",
		"schemas/datapan.adapter-targets.v1.schema.json",
		"schemas/datapan.route-disposition.v1.schema.json",
		"schemas/datapan.providers.v1.schema.json",
		"schemas/datapan.coverage.v1.schema.json",
		"schemas/datapan.verification.v1.schema.json",
		"schemas/datapan.verification-plan.v1.schema.json",
		"schemas/datapan.verification-summary.v1.schema.json",
		"schemas/datapan.release-manifest.v1.schema.json",
		"schemas/datapan.release-verification.v1.schema.json",
		"schemas/datapan.release-readiness.v1.schema.json",
		"schemas/datapan.schema-index.v1.schema.json",
		"schemas/datapan.catalog-diff.v1.schema.json",
		"schemas/datapan.error-catalog.v1.schema.json",
		"schemas/datapan.catalog-audit.v1.schema.json",
		"schemas/datapan.provider-index.v1.schema.json",
		"schemas/datapan.studio-datasets.v1.schema.json",
		"schemas/datapan.studio-bundle.v1.schema.json",
		"schemas/index.json",
		"data/data-go-kr.registry.json",
		"data/provider-index.json",
		"reports/catalog-audit.json",
		"reports/dependencies.json",
		"reports/adapter-targets.json",
		"reports/route-disposition.json",
		"reports/provider-backlog.json",
		"reports/coverage.json",
		"reports/verification-plan.json",
		"reports/latest-verification.json",
		"reports/latest-verification-summary.json",
		"reports/latest-release-verification.json",
		"reports/latest-release-readiness.json",
		"RELEASE_NOTES.md",
		"manifest.json",
		"datapan catalog release draft",
		"datapan catalog release verify",
		"datapan catalog release readiness",
		"--output reports/latest-release-verification.json",
		"datapan catalog update data-go-kr",
		"datapan catalog providers",
		"datapan catalog verify",
		"datapan catalog verify plan",
		"datapan catalog verify --input",
		"datapan catalog audit",
		"datapan catalog dependencies",
		"datapan catalog adapter-targets",
		"datapan catalog route-disposition",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("registry release doc should reference %q", want)
		}
	}
}

func TestSpecGovernanceDocReferencesVersionRules(t *testing.T) {
	path := filepath.Clean(filepath.Join("..", "..", "docs", "spec-governance.md"))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"datapan.<contract>.vN.schema.json",
		"https://schemas.datapan.dev/datapan.<contract>.vN.schema.json",
		"schema_version",
		"breaking changes as `v2`",
		"catalog release verify --manifest manifest.json --output reports/latest-release-verification.json",
		"datapan.release-verification.v1",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("spec governance doc should reference %q", want)
		}
	}
}

func TestEcosystemDocReferencesRepositoryContracts(t *testing.T) {
	path := filepath.Clean(filepath.Join("..", "..", "docs", "ecosystem.md"))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"Spec-First Ownership Ladder",
		"`datapan-cli`",
		"`datapan-data`",
		"`datapan-registry`",
		"`datapan-providers`",
		"`datapan-spec`",
		"`datapan-sdk-*`",
		"`datapan-studio`",
		"`datapan-cloud`",
		"schemas/datapan.verification-summary.v1.schema.json",
		"schemas/datapan.verification-plan.v1.schema.json",
		"schemas/datapan.dependencies.v1.schema.json",
		"schemas/datapan.adapter-targets.v1.schema.json",
		"schemas/datapan.route-disposition.v1.schema.json",
		"schemas/datapan.coverage.v1.schema.json",
		"schemas/datapan.release-manifest.v1.schema.json",
		"schemas/datapan.release-verification.v1.schema.json",
		"schemas/datapan.release-readiness.v1.schema.json",
		"schemas/datapan.schema-index.v1.schema.json",
		"schemas/datapan.catalog-diff.v1.schema.json",
		"schemas/datapan.error-catalog.v1.schema.json",
		"schemas/datapan.catalog-audit.v1.schema.json",
		"schemas/datapan.provider-index.v1.schema.json",
		"schemas/datapan.studio-datasets.v1.schema.json",
		"schemas/datapan.studio-bundle.v1.schema.json",
		"docs/spec-governance.md",
		"reports/latest-verification-summary.json",
		"reports/verification-plan.json",
		"reports/dependencies.json",
		"reports/adapter-targets.json",
		"reports/route-disposition.json",
		"reports/coverage.json",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("ecosystem doc should reference %q", want)
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
		"datapan catalog adapter-targets",
		"--status missing",
		"--status adapter",
		"--kind external_endpoint",
		"--provider q-net",
		"openapi.q-net.or.kr",
		"qnet_missing_required_params",
		"Adapter Readiness Bar",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("provider adapters doc should reference %q", want)
		}
	}
}
