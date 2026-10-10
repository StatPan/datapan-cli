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

func TestExtractSelectedHealthOperationPolicyRows(t *testing.T) {
	data := []byte(`{"schema_version":"datapan.operation-observation-policy.v1","artifact_kind":"operation_observation_policy_set","policies":[{"unused":{"payload":[` + strings.TrimSuffix(strings.Repeat("null,", 499), ",") + `,null]}},{"identity":{"operation_id":"selected"}}],"profiles":[],"effect_profiles":[]}`)
	selected := map[healthOperationPolicyRowKey]struct{}{{Section: "policies", Index: 1}: {}}
	rows, schemaVersion, artifactKind, err := extractSelectedHealthOperationPolicyRows(data, selected)
	if err != nil {
		t.Fatal(err)
	}
	if schemaVersion != "datapan.operation-observation-policy.v1" || artifactKind != "operation_observation_policy_set" || len(rows) != 1 {
		t.Fatalf("unexpected selected policy extraction result: schema=%q kind=%q rows=%d", schemaVersion, artifactKind, len(rows))
	}
	row, err := decodeHealthPolicyRow(rows[healthOperationPolicyRowKey{Section: "policies", Index: 1}])
	if err != nil || row["identity"].(map[string]any)["operation_id"] != "selected" {
		t.Fatalf("selected row was not decoded: row=%v err=%v", row, err)
	}
}

func TestHealthOperationPolicyPointerBounds(t *testing.T) {
	for _, test := range []struct {
		pointer string
		valid   bool
	}{
		{"#/policies/0", true},
		{"#/profiles/12/request/response/branches/3", true},
		{"#/effect_profiles/1/effect_review", true},
		{"#/policies/00", false},
		{"#/policies/-1", false},
		{"#/policies/1e2", false},
		{"#/not_policies/0", false},
		{"policies/0", false},
		{"#/policies/0/", true},
	} {
		_, _, valid := parseHealthOperationPolicyPointer(test.pointer)
		if valid != test.valid {
			t.Errorf("parseHealthOperationPolicyPointer(%q) valid=%v, want %v", test.pointer, valid, test.valid)
		}
	}
}

func TestReviewedReadOnlyMethodMustMatchOperationSpecificEvidence(t *testing.T) {
	plan, policy, documents, documentPath := validReviewedReadOnlyEffectFixture(t, "GET")
	if err := validateSelectedHealthOperationEffectPolicy(plan, policy, documents); err != nil {
		t.Fatalf("explicit GET evidence was rejected: %v", err)
	}

	plan.RequestPlan.RequestContract.Transport.HTTPMethod = "HEAD"
	if err := validateSelectedHealthOperationEffectPolicy(plan, policy, documents); err == nil {
		t.Fatal("HEAD was substituted for source-documented GET")
	}

	plan.RequestPlan.RequestContract.Transport.HTTPMethod = "HEAD"
	row := policy.Rows[healthOperationPolicyRowKey{Section: "profiles", Index: 0}]
	selector := row["selector"].(map[string]any)
	selector["method"] = "HEAD"
	documents[documentPath]["transport"].(map[string]any)["http_method"].(map[string]any)["value"] = "HEAD"
	if err := validateSelectedHealthOperationEffectPolicy(plan, policy, documents); err != nil {
		t.Fatalf("exact source-documented HEAD evidence was rejected: %v", err)
	}
}

func TestDocumentedReadOnlyEffectRequiresExactSourceAndIdentity(t *testing.T) {
	plan, documents, _ := validDocumentedOperationEffectFixture(t)
	if err := validateSelectedHealthOperationEffectPolicy(plan, healthSelectedOperationPolicy{}, documents); err != nil {
		t.Fatalf("exact source-documented read-only effect was rejected: %v", err)
	}

	tests := []struct {
		name string
	}{
		{"unknown effect"},
		{"changed classification"},
		{"missing effect source reference"},
		{"cross-operation identity"},
		{"method mismatch"},
		{"unsupported evidence version"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			freshPlan, freshDocuments, freshPath := validDocumentedOperationEffectFixture(t)
			fresh := freshDocuments[freshPath]
			switch test.name {
			case "unknown effect":
				fresh["effect"].(map[string]any)["status"] = "unknown"
			case "changed classification":
				fresh["effect"].(map[string]any)["classification"] = nil
			case "missing effect source reference":
				fresh["effect"].(map[string]any)["source_refs"] = []any{}
			case "cross-operation identity":
				fresh["identity"].(map[string]any)["operation_id"] = "another-operation"
			case "method mismatch":
				fresh["transport"].(map[string]any)["http_method"].(map[string]any)["value"] = "HEAD"
			case "unsupported evidence version":
				fresh["schema_version"] = "datapan.operation-document-evidence.v1"
			}
			if err := validateSelectedHealthOperationEffectPolicy(freshPlan, healthSelectedOperationPolicy{}, freshDocuments); err == nil {
				t.Fatal("unsupported source effect or identity was accepted")
			}
		})
	}

	plan.RequestPlan.RequestContract.OperationEffect.EvidenceRefs = append(plan.RequestPlan.RequestContract.OperationEffect.EvidenceRefs, plan.RequestPlan.RequestContract.OperationEffect.EvidenceRefs[0])
	if err := validateSelectedHealthOperationEffectPolicy(plan, healthSelectedOperationPolicy{}, documents); err == nil {
		t.Fatal("duplicated effect references were accepted")
	}
	plan, documents, _ = validDocumentedOperationEffectFixture(t)
	plan.RequestPlan.RequestContract.OperationEffect.Authority = "operation_specific_declaration"
	if err := validateSelectedHealthOperationEffectPolicy(plan, healthSelectedOperationPolicy{}, documents); err == nil {
		t.Fatal("unmapped operation-specific declaration authority was accepted")
	}
}

func TestLoadSelectedHealthOperationPolicyValidatesOnlySelectedRow(t *testing.T) {
	root := t.TempDir()
	policyPath := filepath.Join(root, filepath.FromSlash(healthOperationPolicyArtifactPath))
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	policy := map[string]any{
		"schema_version": "datapan.operation-observation-policy.v1",
		"artifact_kind":  "operation_observation_policy_set",
		"policies": []any{
			map[string]any{"unselected_oversized_or_unreviewed_row": true},
			validHealthOperationPolicyTestRow(),
		},
		"profiles": []any{},
	}
	data, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policyPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	sha := hex.EncodeToString(digest[:])
	plan := healthOperationPlanRecord{}
	plan.OperationIdentity.OperationID = "selected-op"
	plan.RequestPlan.EvidenceRefs = []healthOperationPlanEvidenceRef{{
		ArtifactPath: healthOperationPolicyArtifactPath,
		SHA256:       sha,
		JSONPointer:  "#/policies/1",
		EvidenceKind: "reviewed_policy",
	}}
	manifest := releaseManifest{Artifacts: []releaseManifestArtifact{{Path: healthOperationPolicyArtifactPath, Bytes: int64(len(data)), SHA256: sha}}}

	selected, err := loadSelectedHealthOperationPolicy(root, plan, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected.Rows) != 1 || selected.Rows[healthOperationPolicyRowKey{Section: "policies", Index: 1}]["identity"] == nil {
		t.Fatalf("loaded policy did not retain exactly the selected row: %#v", selected.Rows)
	}
	if _, ok := selected.resolve("#/policies/1/request/limits/timeout_ms"); !ok {
		t.Fatal("selected policy pointer did not resolve")
	}
	if _, ok := selected.resolve("#/policies/0/unselected_oversized_or_unreviewed_row"); ok {
		t.Fatal("unselected policy row was accidentally decoded")
	}
}

func TestLoadSelectedHealthOperationPolicyAcceptsObservationOnlyProfileArm(t *testing.T) {
	root := t.TempDir()
	policyPath := filepath.Join(root, filepath.FromSlash(healthOperationPolicyArtifactPath))
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	profile := map[string]any{
		"profile_id": "synthetic-observation-only",
		"selector": map[string]any{
			"source_id": "data_go_kr", "provider": "data.go.kr", "protocol": "REST", "effect": "read_only", "method": "GET",
			"authentication": map[string]any{"requirement": "none", "mechanism": "none", "placement": "none", "parameter_name": nil},
		},
		"review": map[string]any{"review_ref": "https://example.invalid/review", "reviewed_by": "test reviewer", "rationale": "Safe request shape; response semantics remain unknown."},
		"request": map[string]any{
			"parameter_strategies": []any{}, "omit_unmapped_optional_parameters": true,
			"limits":   map[string]any{"request_budget": 1, "timeout_ms": 1000, "max_request_bytes": 1024, "max_response_bytes": 1024},
			"response": map[string]any{"mode": "observation_only"},
		},
	}
	encodePolicy := func(response map[string]any) []byte {
		t.Helper()
		profile["request"].(map[string]any)["response"] = response
		data, err := json.Marshal(map[string]any{
			"schema_version":  "datapan.operation-observation-policy.v1",
			"artifact_kind":   "operation_observation_policy_set",
			"policies":        []any{},
			"profiles":        []any{profile},
			"effect_profiles": []any{},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(policyPath, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return data
	}
	data := encodePolicy(map[string]any{"mode": "observation_only"})
	digest := sha256.Sum256(data)
	sha := hex.EncodeToString(digest[:])
	plan := healthOperationPlanRecord{}
	plan.OperationIdentity.OperationID = "selected-op"
	plan.RequestPlan.EvidenceRefs = []healthOperationPlanEvidenceRef{{
		ArtifactPath: healthOperationPolicyArtifactPath,
		SHA256:       sha,
		JSONPointer:  "#/profiles/0",
		EvidenceKind: "reviewed_policy",
	}}
	manifest := releaseManifest{Artifacts: []releaseManifestArtifact{{Path: healthOperationPolicyArtifactPath, Bytes: int64(len(data)), SHA256: sha}}}
	selected, err := loadSelectedHealthOperationPolicy(root, plan, manifest)
	if err != nil {
		t.Fatalf("pinned policy schema rejected a reusable observation-only profile: %v", err)
	}
	request := selected.Rows[healthOperationPolicyRowKey{Section: "profiles", Index: 0}]["request"].(map[string]any)
	if response := request["response"].(map[string]any); len(response) != 1 || response["mode"] != "observation_only" {
		t.Fatalf("observation-only profile arm was not preserved exactly: %#v", response)
	}

	data = encodePolicy(map[string]any{"mode": "observation_only", "payload_kind": "json"})
	digest = sha256.Sum256(data)
	sha = hex.EncodeToString(digest[:])
	plan.RequestPlan.EvidenceRefs[0].SHA256 = sha
	manifest.Artifacts[0].Bytes = int64(len(data))
	manifest.Artifacts[0].SHA256 = sha
	if _, err := loadSelectedHealthOperationPolicy(root, plan, manifest); err == nil {
		t.Fatal("observation-only profile arm accepted typed response predicates")
	}
}

func TestLoadSelectedHealthOperationPolicyCapsArtifactBeforeReading(t *testing.T) {
	plan := healthOperationPlanRecord{}
	plan.OperationIdentity.OperationID = "selected-op"
	plan.RequestPlan.EvidenceRefs = []healthOperationPlanEvidenceRef{{
		ArtifactPath: healthOperationPolicyArtifactPath,
		SHA256:       strings.Repeat("a", 64),
		JSONPointer:  "#/policies/0",
		EvidenceKind: "reviewed_policy",
	}}
	manifest := releaseManifest{Artifacts: []releaseManifestArtifact{{Path: healthOperationPolicyArtifactPath, Bytes: healthOperationPolicyMaxBytes + 1, SHA256: strings.Repeat("a", 64)}}}
	if _, err := loadSelectedHealthOperationPolicy(t.TempDir(), plan, manifest); err == nil {
		t.Fatal("oversized selected policy artifact was accepted")
	}
}

func TestHealthOperationPlanRuntimeSelectedReadCeilingsMatchDocument(t *testing.T) {
	selectedFileBytes := int64(4<<20) + registryInstallProvenanceMaxBytes + int64(5<<20) +
		healthOperationPlanIndexMaxBytes + healthOperationPlanShardMaxBytes +
		int64(healthOperationPlanMaxSelectedEvidenceArtifacts)*healthOperationDocumentEvidenceMaxBytes +
		healthOperationPolicyMaxBytes + healthOperationResponseAssertionMaxBytes + healthCredentialBindingsMaxBytes
	boundedFileReads := int64(1 + 1 + 5 + 1 + 1 + healthOperationPlanMaxSelectedEvidenceArtifacts + 1 + 1 + 1)
	if want := int64(46<<20) + 320<<10; selectedFileBytes != want {
		t.Fatalf("selected release files have a %d byte ceiling, want %d", selectedFileBytes, want)
	}
	if want := int64(174<<20) + 320<<10 + boundedFileReads + 1; selectedFileBytes+(128<<20)+boundedFileReads+1 != want {
		t.Fatalf("selected release and running-image reads exceed the documented %d byte ceiling", want)
	}
}

func validHealthOperationPolicyTestRow() map[string]any {
	return map[string]any{
		"identity": map[string]any{
			"source_id": "synthetic_source", "operation_id": "selected-op", "provider": "synthetic.invalid", "protocol": "REST",
			"dataset_id": "dataset", "operation_name": "list", "upstream_operation_key": "list",
		},
		"document_evidence": map[string]any{"path": "reports/document.json", "sha256": strings.Repeat("b", 64), "bytes": 1},
		"review":            map[string]any{"review_ref": "https://example.invalid/review", "reviewed_by": "test", "rationale": "test-only reviewed row"},
		"request": map[string]any{
			"parameter_strategies": []any{}, "omit_unmapped_optional_parameters": true,
			"limits":                      map[string]any{"request_budget": 1, "timeout_ms": 1000, "max_request_bytes": 1024, "max_response_bytes": 1024},
			"response_assertion_artifact": map[string]any{"path": "reports/operation-response-assertions/selected-op.json", "sha256": strings.Repeat("c", 64), "bytes": 1},
		},
	}
}

func validReviewedReadOnlyEffectFixture(t *testing.T, method string) (healthOperationPlanRecord, healthSelectedOperationPolicy, map[string]map[string]any, string) {
	t.Helper()
	plan := readHealthOperationPlanFixture(t, "synthetic-rest-list.json")
	plan.SourceBinding.TestOnly = false
	plan.RequestPlan.RequestContract.Transport.HTTPMethod = method
	plan.RequestPlan.RequestContract.OperationEffect.Classification = "read_only"
	plan.RequestPlan.RequestContract.OperationEffect.Authority = "reviewed_policy"
	const documentPath = "reports/operation-document-evidence/synthetic.json"
	const documentSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	plan.RequestPlan.RequestContract.OperationEffect.EvidenceRefs = []healthOperationPlanEvidenceRef{
		{ArtifactPath: healthOperationPolicyArtifactPath, SHA256: strings.Repeat("a", 64), JSONPointer: "#/profiles/0/effect_review", EvidenceKind: "reviewed_policy"},
		{ArtifactPath: healthOperationPolicyArtifactPath, SHA256: strings.Repeat("a", 64), JSONPointer: "#/profiles/0/review", EvidenceKind: "reviewed_policy"},
		{ArtifactPath: healthOperationPolicyArtifactPath, SHA256: strings.Repeat("a", 64), JSONPointer: "#/profiles/0/selector", EvidenceKind: "reviewed_policy"},
		{ArtifactPath: documentPath, SHA256: documentSHA, JSONPointer: "#/transport/http_method", EvidenceKind: "operation_document"},
		{ArtifactPath: documentPath, SHA256: documentSHA, JSONPointer: "#/operation_document/title", EvidenceKind: "operation_document"},
		{ArtifactPath: documentPath, SHA256: documentSHA, JSONPointer: "#/operation_document/purpose", EvidenceKind: "operation_document"},
	}
	plan.OperationIdentity.Protocol = "REST"
	plan.SourceBinding.SourceID = "source"
	plan.SourceBinding.Provider = "provider"
	plan.OperationIdentity.OperationID = "operation"

	policy := healthSelectedOperationPolicy{Rows: map[healthOperationPolicyRowKey]map[string]any{
		{Section: "profiles", Index: 0}: {
			"selector": map[string]any{"source_id": "source", "provider": "provider", "protocol": "REST", "method": method, "effect": "read_only"},
			"review":   map[string]any{"review_ref": "review", "reviewed_by": "reviewer", "rationale": "Read-only retrieval purpose."},
			"effect_review": map[string]any{
				"classification": "read_only", "basis": "rfc9110_safe_method_and_retrieval_purpose",
				"rfc_reference": "https://www.rfc-editor.org/rfc/rfc9110#section-9.2.1",
				"rationale":     "Read-only retrieval purpose.", "purpose_terms": []any{"list"},
			},
		},
	}}
	methodFact := map[string]any{"status": "documented", "authority_scope": "operation_specific", "value": method, "source_refs": []any{map[string]any{"evidence_kind": "operation_http_method"}}}
	titleFact := map[string]any{"status": "documented", "value": "List records", "source_refs": []any{map[string]any{"evidence_kind": "official_operation_title"}}}
	purposeFact := map[string]any{"status": "documented", "value": "List records", "source_refs": []any{map[string]any{"evidence_kind": "official_operation_purpose"}}}
	documents := map[string]map[string]any{documentPath: {
		"identity":           map[string]any{"source_id": "source", "provider": "provider", "operation_id": "operation", "protocol": "REST"},
		"effect":             map[string]any{"status": "unknown"},
		"transport":          map[string]any{"http_method": methodFact, "fixed_query_selectors": []any{}},
		"operation_document": map[string]any{"title": titleFact, "purpose": purposeFact},
		"parameters":         []any{},
	}}
	return plan, policy, documents, documentPath
}

func validDocumentedOperationEffectFixture(t *testing.T) (healthOperationPlanRecord, map[string]map[string]any, string) {
	t.Helper()
	plan, _, documents, documentPath := validReviewedReadOnlyEffectFixture(t, "GET")
	plan.RequestPlan.RequestContract.OperationEffect.Authority = "operation_document"
	plan.RequestPlan.RequestContract.OperationEffect.EvidenceRefs = []healthOperationPlanEvidenceRef{{
		ArtifactPath: documentPath, SHA256: strings.Repeat("b", 64), JSONPointer: "#/effect", EvidenceKind: "operation_document",
	}}
	plan.RequestPlan.RequestContract.Transport.EvidenceRefs = []healthOperationPlanEvidenceRef{{
		ArtifactPath: documentPath, SHA256: strings.Repeat("b", 64), JSONPointer: "#/transport/http_method", EvidenceKind: "operation_document",
	}}
	document := documents[documentPath]
	document["schema_version"] = "datapan.operation-document-evidence.v2"
	identity := document["identity"].(map[string]any)
	identity["dataset_id"] = plan.OperationIdentity.DatasetID
	identity["operation_name"] = plan.OperationIdentity.OperationName
	identity["upstream_operation_key"] = plan.OperationIdentity.UpstreamOperationKey
	effect := map[string]any{"status": "documented", "classification": "read_only", "authority": "operation_document", "source_refs": []any{map[string]any{"evidence_kind": "operation_effect"}}}
	document["effect"] = effect
	return plan, documents, documentPath
}
