package cli

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManifestBoundHealthCatalogSkipsMonolithAndResolvesTenOperations(t *testing.T) {
	root, catalogPath := setupManifestBoundHealthCatalog(t)
	output := filepath.Join(root, "receipt.json")
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("serviceKey") != "credential-secret" || req.URL.Query().Get("pageNo") != "1" {
			t.Fatalf("unexpected bounded request")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"response":{"header":{"resultCode":"00"},"body":{"items":[]}}}`))}, nil
	})
	code, _, stderr := runTest([]string{"verify", "--ref", "15000001", "--operation", "operation-1", "--health", "--health-catalog", catalogPath, "--health-registry-revision", strings.Repeat("a", 40), "--timeout", "10s", "--output", output, "--json"}, fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "credential-secret"}, client)
	if code != exitOK || stderr != "" {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	assertHealthReceipt(t, output, "healthy", "empty")
	var receipt healthProbeReceipt
	data, err := os.ReadFile(output)
	if err != nil || json.Unmarshal(data, &receipt) != nil {
		t.Fatalf("read receipt policy: %v", err)
	}
	if receipt.Policy == nil || receipt.Policy.Key != "dpr-op-00000001" || receipt.Policy.Version != 1 || receipt.Policy.Authority != "datapan-registry" || receipt.Policy.MaxLevel != "L4" {
		t.Fatalf("manifest-bound policy missing from receipt: %#v", receipt.Policy)
	}
	if strings.Contains(string(data), "credential-secret") {
		t.Fatal("manifest-bound receipt is unavailable or unsafe")
	}
}

func TestManifestBoundHealthCatalogAcceptsElevenEntries(t *testing.T) {
	root, catalogPath := setupManifestBoundHealthCatalogWithEntries(t, 11)
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("pageNo") != "1" {
			t.Fatal("eleventh operation did not use its bounded parameters")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"response":{"header":{"resultCode":"00"},"body":{"items":[]}}}`))}, nil
	})
	output := filepath.Join(root, "receipt.json")
	code, _, stderr := runTest([]string{"verify", "--ref", "15000011", "--operation", "operation-11", "--health", "--health-catalog", catalogPath, "--health-registry-revision", strings.Repeat("a", 40), "--timeout", "10s", "--output", output, "--json"}, fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "credential-secret"}, client)
	if code != exitOK || stderr != "" {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	assertHealthReceipt(t, output, "healthy", "empty")
}

func TestManifestBoundHealthCatalogRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, path string)
		want   string
	}{
		{name: "empty", change: func(t *testing.T, path string) {
			if err := os.WriteFile(path, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}, want: "bounded file is unavailable"},
		{name: "empty entries", change: func(t *testing.T, path string) {
			rewriteBoundCatalog(t, path, func(c *manifestHealthCatalog) { c.Entries = []manifestHealthCatalogEntry{} })
		}, want: "health catalog contract is invalid"},
		{name: "duplicate identity", change: func(t *testing.T, path string) {
			rewriteBoundCatalog(t, path, func(c *manifestHealthCatalog) { c.Entries = append(c.Entries, c.Entries[0]) })
		}, want: "health catalog entry policy is invalid"},
		{name: "duplicate selector", change: func(t *testing.T, path string) {
			rewriteBoundCatalog(t, path, func(c *manifestHealthCatalog) {
				entry := c.Entries[0]
				entry.OperationID = "dpr-op-00000011"
				entry.Policy.Key = entry.OperationID
				c.Entries = append(c.Entries, entry)
			})
		}, want: "health catalog selector is invalid"},
		{name: "excess entry count", change: func(t *testing.T, path string) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var catalog manifestHealthCatalog
			if err := json.Unmarshal(data, &catalog); err != nil {
				t.Fatal(err)
			}
			entries := strings.TrimSuffix(strings.Repeat("null,", healthCatalogMaxEntries+1), ",")
			replacement := []byte(fmt.Sprintf(`{"schema_version":%q,"authority":%q,"source_registry":{"sha256":%q},"entries":[%s]}`, catalog.SchemaVersion, catalog.Authority, catalog.SourceRegistry.SHA256, entries))
			writeBoundCatalogAndUpdatePins(t, path, replacement)
		}, want: "health catalog contract is invalid"},
		{name: "oversized file", change: func(t *testing.T, path string) {
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := file.Truncate(healthCatalogMaxBytes + 1); err != nil {
				_ = file.Close()
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
		}, want: "bounded file is unavailable"},
		{name: "incomplete JSON", change: func(t *testing.T, path string) {
			writeBoundCatalogAndUpdatePins(t, path, []byte(`{"schema_version":"datapan.health-probe-catalog.v1","entries":[`))
		}, want: "decode health catalog"},
		{name: "incomplete entry", change: func(t *testing.T, path string) {
			rewriteBoundCatalog(t, path, func(c *manifestHealthCatalog) { c.Entries[0].Execution.RequestBudget = 0 })
		}, want: "health catalog entry policy is invalid"},
		{name: "trailing JSON", change: func(t *testing.T, path string) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			writeBoundCatalogAndUpdatePins(t, path, append(data, []byte(` {}`)...))
		}, want: "decode health catalog"},
		{name: "manifest hash pin", change: func(t *testing.T, path string) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data = append(data, ' ')
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
		}, want: "health catalog is not bound to the installed Registry release"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, path := setupManifestBoundHealthCatalog(t)
			tt.change(t, path)
			_, _, err := loadManifestBoundHealthCatalog(healthCatalogOptions{Path: path, RegistryRevision: strings.Repeat("a", 40)}, time.Now().UTC())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestHealthCatalogPreflightRejectsNullEntryCountBomb(t *testing.T) {
	const nullEntryCount = healthCatalogMaxEntries * 16
	entries := strings.TrimSuffix(strings.Repeat("null,", nullEntryCount), ",")
	data := []byte(`{"entries":[` + entries + `]}`)
	if err := preflightHealthCatalogJSON(data); !errors.Is(err, errHealthCatalogEntryLimit) {
		t.Fatalf("preflight error=%v, want entry limit error", err)
	}
}

func TestHealthCatalogPreflightRejectsCaseFoldedEntryCountBombs(t *testing.T) {
	var decoded struct {
		Entries []manifestHealthCatalogEntry `json:"entries"`
	}
	if err := json.Unmarshal([]byte(`{"ENTRIES":[{"EXECUTION":{"SAFE_PARAMETERS":[null]}}]}`), &decoded); err != nil || len(decoded.Entries) != 1 || len(decoded.Entries[0].Execution.SafeParameters) != 1 {
		t.Fatalf("test case no longer matches encoding/json field names: entries=%d err=%v", len(decoded.Entries), err)
	}

	const nullEntryCount = healthCatalogMaxEntries * 16
	entries := strings.TrimSuffix(strings.Repeat("null,", nullEntryCount), ",")
	for _, data := range [][]byte{
		[]byte(`{"ENTRIES":[` + entries + `]}`),
		[]byte(`{"entries":[null],"EnTrIeS":[` + entries + `]}`),
	} {
		if err := preflightHealthCatalogJSON(data); !errors.Is(err, errHealthCatalogEntryLimit) {
			t.Fatalf("preflight error=%v, want entry limit error", err)
		}
	}
}

func TestHealthCatalogPreflightBoundsSafeParameterArrays(t *testing.T) {
	parametersAtLimit := strings.TrimSuffix(strings.Repeat("null,", healthCatalogMaxSafeParametersPerEntry), ",")
	validData := []byte(`{"entries":[{"execution":{"safe_parameters":[` + parametersAtLimit + `]}}]}`)
	if err := preflightHealthCatalogJSON(validData); err != nil {
		t.Fatalf("preflight rejected per-entry limit: %v", err)
	}

	parameterBomb := strings.TrimSuffix(strings.Repeat("null,", healthCatalogMaxSafeParametersPerEntry+1), ",")
	data := []byte(`{"ENTRIES":[{"EXECUTION":{"SAFE_PARAMETERS":[` + parameterBomb + `]}}]}`)
	if err := preflightHealthCatalogJSON(data); !errors.Is(err, errHealthCatalogSafeParameterLimit) {
		t.Fatalf("preflight error=%v, want safe parameter limit error", err)
	}

	firstArray := strings.TrimSuffix(strings.Repeat("null,", healthCatalogMaxSafeParametersPerEntry-1), ",")
	secondArray := strings.TrimSuffix(strings.Repeat("null,", 2), ",")
	duplicateFieldData := []byte(`{"entries":[{"execution":{"safe_parameters":[` + firstArray + `],"SAFE_PARAMETERS":[` + secondArray + `]}}]}`)
	if err := preflightHealthCatalogJSON(duplicateFieldData); !errors.Is(err, errHealthCatalogSafeParameterLimit) {
		t.Fatalf("duplicate-field preflight error=%v, want safe parameter limit error", err)
	}
}

func TestHealthCatalogPreflightBoundsTotalSafeParameterRecords(t *testing.T) {
	const parametersPerEntry = healthCatalogMaxSafeParametersPerEntry
	const entryCount = healthCatalogMaxTotalSafeParameters/parametersPerEntry + 1
	parameters := strings.TrimSuffix(strings.Repeat("null,", parametersPerEntry), ",")
	entry := `{"execution":{"safe_parameters":[` + parameters + `]}}`
	entriesAtLimit := strings.TrimSuffix(strings.Repeat(entry+",", entryCount-1), ",")
	validData := []byte(`{"entries":[` + entriesAtLimit + `]}`)
	if err := preflightHealthCatalogJSON(validData); err != nil {
		t.Fatalf("preflight rejected aggregate limit: %v", err)
	}
	entriesOverLimit := strings.TrimSuffix(strings.Repeat(entry+",", entryCount), ",")
	data := []byte(`{"entries":[` + entriesOverLimit + `]}`)
	if err := preflightHealthCatalogJSON(data); !errors.Is(err, errHealthCatalogSafeParameterLimit) {
		t.Fatalf("preflight error=%v, want total safe parameter limit error", err)
	}
}

func TestManifestBoundHealthCatalogRejectsTamperBeforeProviderExecution(t *testing.T) {
	_, catalogPath := setupManifestBoundHealthCatalog(t)
	if err := os.WriteFile(catalogPath, []byte(`{"schema_version":"datapan.health-probe-catalog.v1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	client := roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("tampered catalog must stop before provider execution")
		return nil, nil
	})
	code, _, stderr := runTest([]string{"verify", "--ref", "15000001", "--operation", "operation-1", "--health", "--health-catalog", catalogPath, "--health-registry-revision", strings.Repeat("a", 40), "--output", filepath.Join(t.TempDir(), "receipt.json"), "--json"}, fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "credential-secret"}, client)
	if code != exitUsage || !strings.Contains(stderr, "health catalog is not ready") {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
}

func setupManifestBoundHealthCatalog(t *testing.T) (string, string) {
	return setupManifestBoundHealthCatalogWithEntries(t, 10)
}

func setupManifestBoundHealthCatalogWithEntries(t *testing.T, entryCount int) (string, string) {
	t.Helper()
	root := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if err := os.MkdirAll(filepath.Dir(defaultReleaseManifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	registryData := []byte(`not-json-monolith-that-must-not-be-loaded`)
	if err := os.WriteFile(defaultRegistryPath, registryData, 0o600); err != nil {
		t.Fatal(err)
	}
	registrySum := sha256.Sum256(registryData)

	catalog := manifestHealthCatalog{SchemaVersion: healthCatalogSchema, Authority: "datapan-registry"}
	catalog.SourceRegistry.SHA256 = fmt.Sprintf("%x", registrySum)
	for i := 1; i <= entryCount; i++ {
		var entry manifestHealthCatalogEntry
		entry.OperationID = fmt.Sprintf("dpr-op-%08d", i)
		entry.Policy.Key, entry.Policy.Version, entry.Policy.Authority, entry.Policy.MaxLevel = entry.OperationID, 1, "datapan-registry", "L4"
		entry.Aliases.DatasetID = fmt.Sprintf("150000%02d", i)
		entry.Aliases.OperationName = fmt.Sprintf("operation-%d", i)
		entry.Provider = "data.go.kr"
		entry.Endpoint.Host = "apis.data.go.kr"
		entry.Endpoint.Path = fmt.Sprintf("/service/operation-%d", i)
		entry.Endpoint.DependencyClass = "data_go_kr_gateway"
		entry.Eligibility.Status = "credential_required"
		entry.Execution.TimeoutCeilingMS, entry.Execution.RequestBudget = 10000, 1
		entry.Execution.SafeParameters = []manifestHealthParameter{{Name: "pageNo", Strategy: "bounded_integer", Minimum: 1, Maximum: 1}}
		op := healthProbeOperation{DatasetID: entry.Aliases.DatasetID, OperationName: entry.Aliases.OperationName, Provider: entry.Provider, EndpointHost: entry.Endpoint.Host, EndpointPath: entry.Endpoint.Path, DependencyClass: entry.Endpoint.DependencyClass}
		entry.Aliases.CLIOperationKey = healthOperationKey(op)
		catalog.Entries = append(catalog.Entries, entry)
	}
	catalogData, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(root, "health-probe-catalog.json")
	if err := os.WriteFile(catalogPath, catalogData, 0o600); err != nil {
		t.Fatal(err)
	}
	catalogSum := sha256.Sum256(catalogData)
	manifest := releaseManifest{SchemaVersion: "datapan.release-manifest.v1", ArtifactCount: 2, Artifacts: []releaseManifestArtifact{
		{Path: "data/data-go-kr.registry.json", Kind: "registry", Bytes: int64(len(registryData)), SHA256: fmt.Sprintf("%x", registrySum)},
		{Path: healthCatalogArtifactPath, Kind: "health_probe_catalog", Bytes: int64(len(catalogData)), SHA256: fmt.Sprintf("%x", catalogSum)},
	}}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defaultReleaseManifestPath, manifestData, 0o600); err != nil {
		t.Fatal(err)
	}
	manifestSum := sha256.Sum256(manifestData)
	verified := true
	revision := strings.Repeat("a", 40)
	provenance := registryInstallProvenance{SchemaVersion: "datapan.registry-install.v1", Provider: "datapan-registry", RegistryPath: defaultRegistryPath, RegistrySHA256: fmt.Sprintf("%x", registrySum), ReleaseTag: revision, AssetURL: "https://example.test/registry.zip", PinMode: "pinned", SourceMode: "default_installed", Distribution: "huggingface_dataset", DatasetID: "StatPan/datapan-registry", DatasetRevision: revision, DatasetManifestURL: "https://example.test/manifest", DatasetManifestSHA256: strings.Repeat("b", 64), ReleaseManifestSHA256: fmt.Sprintf("%x", manifestSum), ManifestRegistryVerified: &verified}
	if err := writeJSONFile(defaultRegistryInstallProvenancePath, provenance); err != nil {
		t.Fatal(err)
	}
	return root, catalogPath
}
