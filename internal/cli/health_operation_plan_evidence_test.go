package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelectedOperationDocumentEvidenceBindsPlanAndPointers(t *testing.T) {
	root, plan, index, manifest := setupSyntheticOperationDocumentEvidence(t)
	if data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(index.GenerationInputs.DocumentEvidence[0].Path))); err != nil {
		t.Fatal(err)
	} else if _, err := decodeHealthOperationDocumentEvidence(data); err != nil {
		t.Fatalf("synthetic evidence fixture is invalid: %v", err)
	}
	if err := validateSelectedHealthOperationDocumentEvidence(root, plan, index, manifest); err != nil {
		t.Fatalf("valid synthetic evidence was rejected: %v", err)
	}

	plan.RequestPlan.RequestContract.Transport.Host = "other.example.invalid"
	if err := validateSelectedHealthOperationDocumentEvidence(root, plan, index, manifest); err == nil {
		t.Fatal("plan endpoint was accepted after it drifted from its document evidence")
	}
}

func TestSelectedOperationDocumentEvidenceRejectsBadBindingSchemaAndPointer(t *testing.T) {
	t.Run("hash mismatch", func(t *testing.T) {
		root, plan, index, manifest := setupSyntheticOperationDocumentEvidence(t)
		path := filepath.Join(root, filepath.FromSlash(index.GenerationInputs.DocumentEvidence[0].Path))
		if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := validateSelectedHealthOperationDocumentEvidence(root, plan, index, manifest); err == nil {
			t.Fatal("altered sidecar bytes were accepted")
		}
	})

	t.Run("invalid schema", func(t *testing.T) {
		root, plan, index, manifest := setupSyntheticOperationDocumentEvidence(t)
		document := syntheticOperationDocumentEvidence(t)
		document["unexpected"] = true
		writeSyntheticOperationDocumentEvidence(t, root, document, &index, &manifest)
		if err := validateSelectedHealthOperationDocumentEvidence(root, plan, index, manifest); err == nil {
			t.Fatal("sidecar with an unknown field was accepted")
		}
	})

	t.Run("unresolved pointer", func(t *testing.T) {
		root, plan, index, manifest := setupSyntheticOperationDocumentEvidence(t)
		plan.RequestPlan.EvidenceRefs = append(plan.RequestPlan.EvidenceRefs, healthOperationPlanEvidenceRef{
			ArtifactPath: index.GenerationInputs.DocumentEvidence[0].Path,
			SHA256:       index.GenerationInputs.DocumentEvidence[0].SHA256,
			JSONPointer:  "#/transport/not_a_field",
			EvidenceKind: "operation_document",
		})
		if err := validateSelectedHealthOperationDocumentEvidence(root, plan, index, manifest); err == nil {
			t.Fatal("unresolved evidence pointer was accepted")
		}
	})

	t.Run("identity mismatch", func(t *testing.T) {
		root, plan, index, manifest := setupSyntheticOperationDocumentEvidence(t)
		document := syntheticOperationDocumentEvidence(t)
		identity := document["identity"].(map[string]any)
		identity["operation_id"] = strings.Repeat("b", 64)
		writeSyntheticOperationDocumentEvidence(t, root, document, &index, &manifest)
		if err := validateSelectedHealthOperationDocumentEvidence(root, plan, index, manifest); err == nil {
			t.Fatal("sidecar identity for a different operation was accepted")
		}
	})
}

func TestHealthJSONPointerRejectsMalformedEscapes(t *testing.T) {
	document := map[string]any{"a/b": map[string]any{"~key": "value"}}
	if value, ok := healthJSONPointer(document, "#/a~1b/~0key"); !ok || value != "value" {
		t.Fatalf("valid escaped pointer failed: value=%v ok=%t", value, ok)
	}
	if _, ok := healthJSONPointer(document, "#/a~2b"); ok {
		t.Fatal("invalid pointer escape was accepted")
	}
}

func setupSyntheticOperationDocumentEvidence(t *testing.T) (string, healthOperationPlanRecord, healthOperationPlanIndex, releaseManifest) {
	t.Helper()
	root := t.TempDir()
	document := syntheticOperationDocumentEvidence(t)
	index := healthOperationPlanIndex{}
	manifest := releaseManifest{SchemaVersion: "datapan.release-manifest.v1"}
	path := "reports/operation-document-evidence/synthetic-operation.json"
	writeSyntheticOperationDocumentEvidence(t, root, document, &index, &manifest)
	index.GenerationInputs.DocumentEvidence[0].Path = path
	// Re-write with the final canonical path used by both the plan and manifest.
	writeSyntheticOperationDocumentEvidenceAtPath(t, root, path, document, &index, &manifest)

	operationID := strings.Repeat("a", 64)
	ref := func(pointer string) healthOperationPlanEvidenceRef {
		return healthOperationPlanEvidenceRef{ArtifactPath: path, SHA256: index.GenerationInputs.DocumentEvidence[0].SHA256, JSONPointer: pointer, EvidenceKind: "operation_document"}
	}
	plan := healthOperationPlanRecord{
		SourceBinding: healthOperationPlanSourceBinding{SourceID: "synthetic_scope", Provider: "data.go.kr", AdapterID: "synthetic-adapter"},
		OperationIdentity: healthOperationPlanIdentity{
			OperationID: operationID, Protocol: "REST", DatasetID: "12345", OperationName: "Synthetic read", UpstreamOperationKey: "98765",
		},
	}
	plan.RequestPlan.EvidenceRefs = []healthOperationPlanEvidenceRef{ref("#/identity"), ref("#/transport/host"), ref("#/transport/path"), ref("#/transport/scheme"), ref("#/transport/http_method")}
	contract := &healthOperationPlanRequestContract{}
	contract.Transport.Protocol = "REST"
	contract.Transport.Scheme = "https"
	contract.Transport.Host = "api.example.invalid"
	contract.Transport.Path = "/v1/records"
	contract.Transport.HTTPMethod = "GET"
	contract.Transport.Authority = "operation_document"
	contract.Transport.EvidenceRefs = []healthOperationPlanEvidenceRef{ref("#/transport/protocol"), ref("#/transport/scheme"), ref("#/transport/host"), ref("#/transport/path"), ref("#/transport/http_method")}
	plan.RequestPlan.RequestContract = contract
	return root, plan, index, manifest
}

func writeSyntheticOperationDocumentEvidence(t *testing.T, root string, document map[string]any, index *healthOperationPlanIndex, manifest *releaseManifest) {
	t.Helper()
	writeSyntheticOperationDocumentEvidenceAtPath(t, root, "reports/operation-document-evidence/synthetic-operation.json", document, index, manifest)
}

func writeSyntheticOperationDocumentEvidenceAtPath(t *testing.T, root, artifactPath string, document map[string]any, index *healthOperationPlanIndex, manifest *releaseManifest) {
	t.Helper()
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, filepath.FromSlash(artifactPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	digestText := hex.EncodeToString(digest[:])
	ref := healthOperationPlanArtifactRef{Path: artifactPath, SHA256: digestText, Bytes: int64(len(data))}
	if len(index.GenerationInputs.DocumentEvidence) == 0 {
		index.GenerationInputs.DocumentEvidence = []healthOperationPlanArtifactRef{ref}
	} else {
		index.GenerationInputs.DocumentEvidence[0] = ref
	}
	manifest.Artifacts = []releaseManifestArtifact{{Path: artifactPath, SHA256: digestText, Bytes: int64(len(data)), Kind: "operation_document_evidence", Schema: healthOperationDocumentEvidenceSchemaID}}
}

func syntheticOperationDocumentEvidence(t *testing.T) map[string]any {
	t.Helper()
	operationID := strings.Repeat("a", 64)
	sourceRef := []any{}
	fact := func(value, status string) map[string]any {
		return map[string]any{"value": value, "status": status, "source_refs": sourceRef}
	}
	bindings := make([]any, 3)
	for index, sourceID := range []string{"catalogue_page", "operation_detail", "reference_guide"} {
		bindings[index] = map[string]any{
			"source_id":  sourceID,
			"origin":     map[string]any{"scheme": "https", "host": "www.data.go.kr", "path": "/synthetic", "method": "GET", "query_values_stored": false, "body_values_stored": false},
			"media_type": "text/html", "bytes": 1, "sha256": strings.Repeat("0", 64), "retrieved_at": "2026-10-07T00:00:00Z", "capture_role": "synthetic_test",
			"parser": map[string]any{"id": "synthetic-parser", "version": "1.0.0"},
		}
	}
	return map[string]any{
		"schema_version": "datapan.operation-document-evidence.v1",
		"parser":         map[string]any{"id": "data-go-kr-operation-document-parser", "version": "1.0.0"},
		"identity": map[string]any{
			"operation_id": operationID, "provider": "data.go.kr", "dataset_id": "12345", "protocol": "REST", "source_system": "data.go.kr",
			"upstream_operation_key": "98765", "operation_name": "Synthetic read", "source_refs": sourceRef,
		},
		"source_bindings": bindings, "parse_status": "parsed_complete",
		"transport": map[string]any{
			"protocol": fact("REST", "registered_manifest"), "scheme": fact("https", "documented"), "host": fact("api.example.invalid", "documented"), "path": fact("/v1/records", "documented"),
			"http_method": map[string]any{"value": "GET", "status": "documented", "authority_scope": "operation_specific", "source_refs": sourceRef},
			"soap_action": fact("", "not_applicable"), "soap_version": fact("", "not_applicable"), "envelope_namespace": fact("", "not_applicable"), "operation_qname": fact("", "not_applicable"), "body_encoding": fact("", "not_applicable"),
		},
		"effect":         map[string]any{"classification": "read_only", "status": "documented", "authority": "operation_document", "source_refs": sourceRef},
		"parameters":     []any{},
		"authentication": map[string]any{"requirement": "none", "status": "unknown", "mechanism": nil, "parameter_names": []any{}, "placement": nil, "source_refs": sourceRef},
		"limits": map[string]any{
			"provider_quota": map[string]any{"value": nil, "unit": nil, "status": "unknown", "source_refs": sourceRef},
			"request_budget": map[string]any{"value": 1, "status": "not_a_provider_fact", "source_refs": sourceRef},
		},
		"response_assertion": map[string]any{"kind": "unknown", "fields": []any{}, "empty_result_semantics": map[string]any{"value": nil, "status": "unknown", "source_refs": sourceRef}, "source_refs": sourceRef},
		"explicit_unknowns":  []any{"synthetic test evidence only"},
	}
}
