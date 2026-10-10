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

func TestHealthResponseAssertionPartialProviderIdentityContract(t *testing.T) {
	for _, test := range []struct {
		sourceID string
		provider string
	}{
		{sourceID: "ecos", provider: "ECOS"},
		{sourceID: "kosis", provider: "KOSIS"},
		{sourceID: "open_assembly", provider: "open.assembly.go.kr"},
		{sourceID: "seoul_open_data", provider: "data.seoul.go.kr"},
	} {
		t.Run(test.sourceID, func(t *testing.T) {
			instance := healthResponseAssertionIdentitySchemaFixture(test.sourceID, test.provider)
			if err := validateHealthResponseAssertionIdentitySchemaFixture(instance); err != nil {
				t.Fatalf("partial identity without invented source IDs was rejected: %v", err)
			}
			identity := instance["operation_identity"].(map[string]any)
			identity["dataset_id"] = "invented-dataset"
			if err := validateHealthResponseAssertionIdentitySchemaFixture(instance); err == nil {
				t.Fatal("partial source identity accepted an invented dataset ID")
			}
			delete(identity, "dataset_id")
			identity["upstream_operation_key"] = "invented-operation-key"
			if err := validateHealthResponseAssertionIdentitySchemaFixture(instance); err == nil {
				t.Fatal("partial source identity accepted an invented upstream operation key")
			}
		})
	}

	for _, test := range []struct {
		name     string
		sourceID string
		provider string
		mutate   func(map[string]any)
	}{
		{
			name: "data_go_kr requires both source identifiers", sourceID: "data_go_kr", provider: "data.go.kr",
			mutate: func(identity map[string]any) {
				delete(identity, "dataset_id")
				delete(identity, "upstream_operation_key")
			},
		},
		{
			name: "unknown source requires both source identifiers", sourceID: "synthetic_source", provider: "synthetic",
			mutate: func(identity map[string]any) {
				delete(identity, "dataset_id")
				delete(identity, "upstream_operation_key")
			},
		},
		{
			name: "provider label is source-bound", sourceID: "ecos", provider: "KOSIS",
			mutate: func(map[string]any) {},
		},
		{
			name: "source ID is exact-case", sourceID: "ECOS", provider: "ECOS",
			mutate: func(map[string]any) {},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			instance := healthResponseAssertionIdentitySchemaFixture(test.sourceID, test.provider)
			test.mutate(instance["operation_identity"].(map[string]any))
			if err := validateHealthResponseAssertionIdentitySchemaFixture(instance); err == nil {
				t.Fatal("invalid source-scoped identity passed the canonical v2 schema")
			}
		})
	}
}

func TestHealthResponseAssertionIdentityMustExactlyMatchSelectedPlan(t *testing.T) {
	plan := healthOperationPlanRecord{
		SourceBinding: healthOperationPlanSourceBinding{SourceID: "ecos", Provider: "ECOS"},
		OperationIdentity: healthOperationPlanIdentity{
			OperationID: "ecos-read-1", Protocol: "REST", OperationName: "read-series",
		},
	}
	artifact := healthOperationResponseAssertionV2Artifact{}
	artifact.SourceBinding.SourceID = "ecos"
	artifact.SourceBinding.Provider = "ECOS"
	artifact.SourceBinding.Protocol = "REST"
	artifact.OperationIdentity.OperationID = "ecos-read-1"
	artifact.OperationIdentity.OperationName = "read-series"
	if !healthResponseAssertionIdentityMatchesPlan(artifact, plan) {
		t.Fatal("matching partial-provider assertion identity was rejected")
	}

	tests := []struct {
		name   string
		mutate func(*healthOperationResponseAssertionV2Artifact)
	}{
		{"source ID", func(value *healthOperationResponseAssertionV2Artifact) { value.SourceBinding.SourceID = "kosis" }},
		{"provider", func(value *healthOperationResponseAssertionV2Artifact) { value.SourceBinding.Provider = "KOSIS" }},
		{"protocol", func(value *healthOperationResponseAssertionV2Artifact) { value.SourceBinding.Protocol = "SOAP" }},
		{"operation ID", func(value *healthOperationResponseAssertionV2Artifact) { value.OperationIdentity.OperationID = "other" }},
		{"dataset ID", func(value *healthOperationResponseAssertionV2Artifact) {
			value.OperationIdentity.DatasetID = "invented"
		}},
		{"operation name", func(value *healthOperationResponseAssertionV2Artifact) {
			value.OperationIdentity.OperationName = "other"
		}},
		{"upstream operation key", func(value *healthOperationResponseAssertionV2Artifact) {
			value.OperationIdentity.UpstreamOperationKey = "invented"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			altered := artifact
			test.mutate(&altered)
			if healthResponseAssertionIdentityMatchesPlan(altered, plan) {
				t.Fatal("assertion identity mismatch was accepted for the selected plan")
			}
		})
	}
}

func healthResponseAssertionIdentitySchemaFixture(sourceID, provider string) map[string]any {
	identity := map[string]any{"operation_id": "operation-1", "operation_name": "read"}
	if sourceID != "ecos" && sourceID != "kosis" && sourceID != "open_assembly" && sourceID != "seoul_open_data" {
		identity["dataset_id"] = "dataset-1"
		identity["upstream_operation_key"] = "upstream-1"
	}
	return map[string]any{
		"schema_version":     "datapan.operation-response-assertion.v2",
		"artifact_kind":      "operation_response_assertion",
		"source_binding":     map[string]any{"source_id": sourceID, "provider": provider, "protocol": "REST"},
		"operation_identity": identity,
		"document_evidence":  map[string]any{"path": "reports/document.json", "sha256": strings.Repeat("a", 64), "bytes": 1},
		"review":             map[string]any{"review_ref": "https://example.invalid/review", "reviewed_by": "test", "rationale": "source identity contract"},
		"assertion":          map[string]any{"mode": "observation_only"},
	}
}

func validateHealthResponseAssertionIdentitySchemaFixture(instance map[string]any) error {
	encoded, err := json.Marshal(instance)
	if err != nil {
		return err
	}
	return validateHealthOperationResponseAssertionArtifactSchema(encoded)
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
