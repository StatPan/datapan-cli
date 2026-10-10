package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHealthOperationResponseAssertionV2SchemaCompiles(t *testing.T) {
	if _, err := healthOperationResponseAssertionJSONSchema(); err != nil {
		t.Fatal(err)
	}
	if _, err := healthOperationDocumentEvidenceV2JSONSchema(); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeHealthOperationResponseAssertionV2BoundsAndSemantics(t *testing.T) {
	wire := healthOperationResponseAssertionV2Body{PayloadKind: "json", Branches: []healthOperationResponseAssertionV2{{
		ID: "list-success", Classification: "success", EmptyResultSemantics: "valid",
		Selector: healthOperationResponseAssertionV2Selector{
			AcceptedHTTPStatusCodes: []int{200}, RootKind: "object",
			Discriminators: []healthOperationResponseAssertionV2Discriminator{{
				Path:      healthOperationResponseAssertionV2Path{Kind: "json_pointer", Value: "#/code"},
				Predicate: "equals_any", ValueType: "string", Values: []json.RawMessage{json.RawMessage(`"OK"`)},
			}},
		},
		RequiredFields: []healthOperationResponseAssertionV2Field{{
			Path: healthOperationResponseAssertionV2Path{Kind: "json_pointer", Value: "#/items"}, ValueType: "array",
			Cardinality: healthOperationResponseAssertionV2Cardinality{Minimum: 1, Maximum: json.RawMessage("null")},
		}},
		ProviderResultCodeStatus: "expected_success_example",
		ProviderResultCodes: &healthOperationResponseAssertionV2Codes{
			Path: healthOperationResponseAssertionV2Path{Kind: "json_pointer", Value: "#/code"}, ValueType: "string", Basis: "official_success_example",
			SuccessValues: []json.RawMessage{json.RawMessage(`"OK"`)},
		},
		ResultCollection: &healthOperationResponseAssertionV2Collection{
			Path:          &healthOperationResponseAssertionV2Path{Kind: "json_pointer", Value: "#/items"},
			ContainerPath: healthOperationResponseAssertionV2Path{Kind: "json_pointer", Value: "#"},
			ItemPath:      nil, ValueType: "array", Semantics: "valid",
		},
	}}}
	assertion, err := normalizeHealthOperationResponseAssertionV2(wire)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateHealthNormalizedResponseAssertion(assertion); err != nil {
		t.Fatalf("normalized assertion violates evaluator bounds: %v", err)
	}
	if got := assertion.Branches[0].RequiredFields[0].MaximumCount; got != healthResponseAssertionMaxSelectedNodes {
		t.Fatalf("null wire maximum normalized to %d, want the bounded parser ceiling %d", got, healthResponseAssertionMaxSelectedNodes)
	}
	if got := evaluateHealthResponseTest(assertion, 200, `{"code":"OK","items":[]}`); got.Outcome != healthResponseHealthy {
		t.Fatalf("normalized success assertion rejected valid empty response: %#v", got)
	}
	if got := evaluateHealthResponseTest(assertion, 200, `{"code":"UNKNOWN","items":[]}`); got.Outcome != healthResponseIndeterminate {
		t.Fatalf("unknown response code was not kept indeterminate: %#v", got)
	}
}

func TestObservationOnlyResponseAssertionWireArmIsExclusive(t *testing.T) {
	base := map[string]any{
		"schema_version":     "datapan.operation-response-assertion.v2",
		"artifact_kind":      "operation_response_assertion",
		"source_binding":     map[string]any{"source_id": "synthetic_source", "provider": "synthetic", "protocol": "REST"},
		"operation_identity": map[string]any{"operation_id": "synthetic-op", "dataset_id": "dataset", "operation_name": "read", "upstream_operation_key": "operation"},
		"document_evidence":  map[string]any{"path": "reports/doc.json", "sha256": strings.Repeat("a", 64), "bytes": 10},
		"review":             map[string]any{"review_ref": "https://example.invalid/review", "reviewed_by": "test", "rationale": "request construction only"},
		"assertion":          map[string]any{"mode": "observation_only"},
	}
	encoded, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateHealthOperationResponseAssertionArtifactSchema(encoded); err != nil {
		t.Fatalf("valid observation-only assertion arm failed pinned schema: %v", err)
	}
	assertion, err := normalizeHealthOperationResponseAssertionV2(healthOperationResponseAssertionV2Body{Mode: "observation_only"})
	if err != nil || !assertion.ObservationOnly || validateHealthNormalizedResponseAssertion(assertion) != nil {
		t.Fatalf("observation-only assertion did not normalize safely: %#v err=%v", assertion, err)
	}
	plan := healthOperationPlanRecord{}
	plan.RequestPlan.RequestContract = &healthOperationPlanRequestContract{}
	plan.RequestPlan.RequestContract.ResponseAssertion.Kind = "observation_only"
	plan.RequestPlan.RequestContract.ResponseAssertion.EmptyResultSemantics = "not_applicable"
	if err := validateHealthOperationResponseAssertionPlanProjection(plan, assertion); err != nil {
		t.Fatalf("observation-only plan projection was rejected: %v", err)
	}

	base["assertion"] = map[string]any{"mode": "observation_only", "payload_kind": "json", "branches": []any{}}
	encoded, err = json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateHealthOperationResponseAssertionArtifactSchema(encoded); err == nil {
		t.Fatal("observation-only assertion arm accepted typed predicates")
	}
	if _, err := normalizeHealthOperationResponseAssertionV2(healthOperationResponseAssertionV2Body{Mode: "observation_only", PayloadKind: "json"}); err == nil {
		t.Fatal("observation-only normalized arm accepted a payload kind")
	}
}

func TestNormalizeHealthOperationResponseCardinalityUsesNodeCeiling(t *testing.T) {
	minimum, maximum, err := normalizeHealthOperationResponseCardinality(healthOperationResponseAssertionV2Cardinality{Minimum: 1, Maximum: json.RawMessage("50000")})
	if err != nil || minimum != 1 || maximum != healthResponseAssertionMaxSelectedNodes {
		t.Fatalf("valid 50,000 selected-node contract: min=%d max=%d err=%v", minimum, maximum, err)
	}
	if _, _, err := normalizeHealthOperationResponseCardinality(healthOperationResponseAssertionV2Cardinality{Minimum: 1, Maximum: json.RawMessage("50001")}); err == nil {
		t.Fatal("selected-node maximum beyond parser ceiling was accepted")
	}
	if _, _, err := normalizeHealthOperationResponseCardinality(healthOperationResponseAssertionV2Cardinality{Minimum: 1, Maximum: nil}); err == nil {
		t.Fatal("omitted maximum was treated as schema null")
	}
}

func TestHealthResponseAssertionSchemaPathDoesNotAdmitAbsolutePaths(t *testing.T) {
	instance := map[string]any{
		"schema_version":     "datapan.operation-response-assertion.v2",
		"artifact_kind":      "operation_response_assertion",
		"source_binding":     map[string]any{"source_id": "synthetic_source", "provider": "Synthetic", "protocol": "REST"},
		"operation_identity": map[string]any{"operation_id": "synthetic-op", "dataset_id": "synthetic", "operation_name": "read", "upstream_operation_key": "read"},
		"document_evidence":  map[string]any{"path": "reports/doc.json", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bytes": 10},
		"review":             map[string]any{"review_ref": "https://example.invalid/review", "reviewed_by": "test", "rationale": "test"},
		"assertion": map[string]any{
			"payload_kind": "json",
			"branches": []any{map[string]any{
				"branch_id": "success", "classification": "success", "empty_result_semantics": "not_applicable",
				"selector":                map[string]any{"accepted_http_status_codes": []any{200}, "root_kind": "object", "discriminators": []any{}},
				"http_status_source_refs": []any{map[string]any{"artifact_path": "reports/doc.json", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "json_pointer": "#/status", "evidence_kind": "operation_document"}},
				"required_fields": []any{map[string]any{
					"path": map[string]any{"kind": "json_pointer", "value": "#/items"}, "value_type": "array",
					"cardinality": map[string]any{"minimum": 1, "maximum": 1},
					"source_refs": []any{map[string]any{"artifact_path": "reports/doc.json", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "json_pointer": "#/items", "evidence_kind": "operation_document"}},
				}},
				"provider_result_code_status": "none_by_policy", "provider_result_code_evidence_refs": []any{map[string]any{"artifact_path": "reports/doc.json", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "json_pointer": "#/status", "evidence_kind": "operation_document"}},
				"source_refs": []any{map[string]any{"artifact_path": "reports/doc.json", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "json_pointer": "#/status", "evidence_kind": "operation_document"}},
				"review_refs": []any{map[string]any{"artifact_path": "policy/operation-observation-policies.v1.json", "sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "json_pointer": "#/policies/0/request/response_assertion", "evidence_kind": "reviewed_policy"}},
			}},
		},
	}
	encoded, err := json.Marshal(instance)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateHealthOperationResponseAssertionArtifactSchema(encoded); err != nil {
		t.Fatalf("valid relative-path assertion fixture failed schema validation: %v", err)
	}
	instance["document_evidence"].(map[string]any)["path"] = "/etc/passwd"
	encoded, err = json.Marshal(instance)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateHealthOperationResponseAssertionArtifactSchema(encoded); err == nil {
		t.Fatal("absolute artifact path passed the pinned response-assertion schema")
	}
}
