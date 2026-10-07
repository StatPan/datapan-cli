package cli

import (
	"encoding/xml"
	"fmt"
	"strings"
	"testing"
)

func healthJSONAssertionFixture() healthNormalizedResponseAssertion {
	return healthNormalizedResponseAssertion{
		PayloadKind:             "json",
		AcceptedHTTPStatusCodes: []int{httpStatusOKForAssertionTest},
		RequiredFields: []healthNormalizedResponseField{{
			Path:         healthNormalizedResponsePath{JSONPointer: "#/data"},
			ValueType:    "object",
			MinimumCount: 1,
			MaximumCount: 1,
		}},
		ProviderResultCodeMode: "documented",
		ProviderResultCodePath: healthNormalizedResponsePath{JSONPointer: "#/code"},
		ProviderResultCodeType: "string",
		ProviderSuccessCodes:   []healthAssertionScalar{{ValueType: "string", Value: "OK"}},
		ProviderErrorCodes:     []healthAssertionScalar{{ValueType: "string", Value: "ERROR"}},
		ResultCollection: &healthNormalizedResponseCollection{
			JSONPointer:    "#/data/items",
			EmptySemantics: "valid",
		},
	}
}

const httpStatusOKForAssertionTest = 200

func healthXMLAssertionFixture() healthNormalizedResponseAssertion {
	const namespace = "urn:example:response"
	return healthNormalizedResponseAssertion{
		PayloadKind:             "xml",
		AcceptedHTTPStatusCodes: []int{httpStatusOKForAssertionTest},
		RequiredFields: []healthNormalizedResponseField{{
			Path: healthNormalizedResponsePath{XMLPath: []xml.Name{
				{Space: namespace, Local: "Envelope"},
				{Space: namespace, Local: "Data"},
			}},
			ValueType:    "object",
			MinimumCount: 1,
			MaximumCount: 1,
		}},
		ProviderResultCodeMode: "documented",
		ProviderResultCodePath: healthNormalizedResponsePath{XMLPath: []xml.Name{
			{Space: namespace, Local: "Envelope"},
			{Space: namespace, Local: "Code"},
		}},
		ProviderResultCodeType: "string",
		ProviderSuccessCodes:   []healthAssertionScalar{{ValueType: "string", Value: "OK"}},
		ProviderErrorCodes:     []healthAssertionScalar{{ValueType: "string", Value: "ERROR"}},
		ResultCollection: &healthNormalizedResponseCollection{
			XMLContainer: []xml.Name{
				{Space: namespace, Local: "Envelope"},
				{Space: namespace, Local: "Data"},
				{Space: namespace, Local: "Items"},
			},
			XMLItemPath:    []xml.Name{{Space: namespace, Local: "Item"}},
			EmptySemantics: "valid",
		},
	}
}

func healthNormalizedJSONResponseBranchesFixture() []healthNormalizedResponseBranch {
	return []healthNormalizedResponseBranch{
		{
			ID: "success", Classification: "success", PayloadKind: "json",
			AcceptedHTTPStatusCodes: []int{200}, RootKind: "object",
			Discriminators: []healthNormalizedResponseDiscriminator{{
				Path: healthNormalizedResponsePath{JSONPointer: "#/success"}, Predicate: "node_type", ValueType: "object",
			}},
			RequiredFields: []healthNormalizedResponseField{{
				Path: healthNormalizedResponsePath{JSONPointer: "#/success"}, ValueType: "object", MinimumCount: 1, MaximumCount: 1,
			}},
			ProviderResultCodeMode: "none",
			ResultCollection:       &healthNormalizedResponseCollection{JSONPointer: "#/success/items", EmptySemantics: "valid"},
		},
		{
			ID: "error", Classification: "provider_error", PayloadKind: "json",
			AcceptedHTTPStatusCodes: []int{200}, RootKind: "object",
			Discriminators: []healthNormalizedResponseDiscriminator{{
				Path: healthNormalizedResponsePath{JSONPointer: "#/error"}, Predicate: "node_type", ValueType: "object",
			}},
			RequiredFields: []healthNormalizedResponseField{{
				Path: healthNormalizedResponsePath{JSONPointer: "#/error"}, ValueType: "object", MinimumCount: 1, MaximumCount: 1,
			}},
			ProviderResultCodeMode: "none",
		},
	}
}

func evaluateHealthResponseTest(assertion healthNormalizedResponseAssertion, status int, body string) healthResponseAssertionResult {
	return evaluateHealthNormalizedResponseAssertion(assertion, healthHTTPResponse{StatusCode: status, Body: []byte(body)})
}

func TestHealthNormalizedJSONResponseAssertionOutcomes(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantState healthResponseAssertionOutcome
		wantCode  string
	}{
		{name: "documented success and valid empty collection", body: `{"code":"OK","data":{"items":[]}}`, wantState: healthResponseHealthy, wantCode: "response_assertion_passed"},
		{name: "HTTP 200 provider error precedes success shape checks", body: `{"code":"ERROR"}`, wantState: healthResponseUnhealthy, wantCode: "response_provider_error"},
		{name: "unknown code is indeterminate", body: `{"code":"PENDING","data":{"items":[]}}`, wantState: healthResponseIndeterminate, wantCode: "response_provider_code_unknown"},
		{name: "missing code is indeterminate", body: `{"data":{"items":[]}}`, wantState: healthResponseIndeterminate, wantCode: "response_provider_code_unknown"},
		{name: "exact duplicate keys are rejected", body: `{"code":"OK","code":"ERROR","data":{"items":[]}}`, wantState: healthResponseIndeterminate, wantCode: "response_payload_invalid"},
		{name: "escaped alias duplicate keys are rejected", body: `{"code":"OK","\u0063ode":"ERROR","data":{"items":[]}}`, wantState: healthResponseIndeterminate, wantCode: "response_payload_invalid"},
		{name: "case-distinct provider members remain distinct", body: `{"code":"OK","Code":"label","data":{"items":[]}}`, wantState: healthResponseHealthy, wantCode: "response_assertion_passed"},
		{name: "missing result collection is shape drift", body: `{"code":"OK","data":{}}`, wantState: healthResponseIndeterminate, wantCode: "response_shape_mismatch"},
		{name: "wrong result collection type is shape drift", body: `{"code":"OK","data":{"items":{}}}`, wantState: healthResponseIndeterminate, wantCode: "response_shape_mismatch"},
		{name: "wrong provider-code type is indeterminate", body: `{"code":1,"data":{"items":[]}}`, wantState: healthResponseIndeterminate, wantCode: "response_provider_code_unknown"},
		{name: "unpaired surrogate is rejected", body: `{"code":"\uD800","data":{"items":[]}}`, wantState: healthResponseIndeterminate, wantCode: "response_payload_invalid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := evaluateHealthResponseTest(healthJSONAssertionFixture(), 200, test.body)
			if result.Outcome != test.wantState || result.ReasonCode != test.wantCode {
				t.Fatalf("got outcome=%q reason=%q, want %q/%q", result.Outcome, result.ReasonCode, test.wantState, test.wantCode)
			}
		})
	}
	t.Run("invalid UTF-8 is rejected", func(t *testing.T) {
		body := []byte(`{"code":"`)
		body = append(body, 0xff)
		body = append(body, []byte(`","data":{"items":[]}}`)...)
		result := evaluateHealthNormalizedResponseAssertion(healthJSONAssertionFixture(), healthHTTPResponse{StatusCode: 200, Body: body})
		if result.Outcome != healthResponseIndeterminate || result.ReasonCode != "response_payload_invalid" {
			t.Fatalf("invalid UTF-8 was accepted: %#v", result)
		}
	})
}

func TestHealthNormalizedJSONResponseAssertionBoundsAndSemantics(t *testing.T) {
	t.Run("nonaccepted HTTP 200 is unhealthy without parsing body", func(t *testing.T) {
		assertion := healthJSONAssertionFixture()
		assertion.AcceptedHTTPStatusCodes = []int{201}
		result := evaluateHealthResponseTest(assertion, 200, "not-json")
		if result.Outcome != healthResponseUnhealthy || result.ReasonCode != "response_status_not_accepted" {
			t.Fatalf("unexpected status result: %#v", result)
		}
	})
	t.Run("code-free mode still checks response shape", func(t *testing.T) {
		assertion := healthJSONAssertionFixture()
		assertion.ProviderResultCodeMode = "none"
		assertion.ProviderResultCodePath = healthNormalizedResponsePath{}
		assertion.ProviderResultCodeType = ""
		assertion.ProviderSuccessCodes = nil
		assertion.ProviderErrorCodes = nil
		if got := evaluateHealthResponseTest(assertion, 200, `{"data":{"items":[]}}`); got.Outcome != healthResponseHealthy {
			t.Fatalf("valid code-free response did not pass: %#v", got)
		}
		if got := evaluateHealthResponseTest(assertion, 200, `{"data":{}}`); got.Outcome != healthResponseIndeterminate {
			t.Fatalf("code-free mode skipped structural predicates: %#v", got)
		}
	})
	t.Run("invalid empty collection is unhealthy", func(t *testing.T) {
		assertion := healthJSONAssertionFixture()
		assertion.ResultCollection.EmptySemantics = "invalid"
		result := evaluateHealthResponseTest(assertion, 200, `{"code":"OK","data":{"items":[]}}`)
		if result.Outcome != healthResponseUnhealthy || result.ReasonCode != "response_collection_empty" {
			t.Fatalf("unexpected empty collection result: %#v", result)
		}
	})
	t.Run("depth and token ceilings fail closed", func(t *testing.T) {
		assertion := healthJSONAssertionFixture()
		deep := strings.Repeat("[", healthOperationPlanMaxJSONDepth+1) + "0" + strings.Repeat("]", healthOperationPlanMaxJSONDepth+1)
		for _, body := range []string{
			deep,
			`{"code":"OK","data":{"items":[` + strings.Repeat("{},", healthOperationPlanMaxJSONTokens/2) + `{}]}}`,
		} {
			result := evaluateHealthResponseTest(assertion, 200, body)
			if result.Outcome != healthResponseIndeterminate || result.ReasonCode != "response_payload_invalid" {
				t.Fatalf("bounded parser accepted invalid/oversized JSON: %#v", result)
			}
		}
	})
	t.Run("response byte ceiling fails closed", func(t *testing.T) {
		assertion := healthJSONAssertionFixture()
		body := strings.Repeat(" ", int(healthTransportMaxBytes)+1)
		result := evaluateHealthNormalizedResponseAssertion(assertion, healthHTTPResponse{StatusCode: 200, Body: []byte(body)})
		if result.Outcome != healthResponseIndeterminate || result.ReasonCode != "response_body_limit_exceeded" {
			t.Fatalf("oversized response was not indeterminate: %#v", result)
		}
	})
}

func TestHealthNormalizedResponseAssertionTypedScalars(t *testing.T) {
	t.Run("JSON number is distinct from boolean and canonical numeric forms match", func(t *testing.T) {
		assertion := healthNormalizedResponseAssertion{
			PayloadKind:             "json",
			AcceptedHTTPStatusCodes: []int{200},
			ProviderResultCodeMode:  "documented",
			ProviderResultCodePath:  healthNormalizedResponsePath{JSONPointer: "#/code"},
			ProviderResultCodeType:  "integer",
			ProviderSuccessCodes:    []healthAssertionScalar{{ValueType: "integer", Value: "1.0"}},
			ProviderErrorCodes:      []healthAssertionScalar{{ValueType: "integer", Value: "2"}},
		}
		if got := evaluateHealthResponseTest(assertion, 200, `{"code":1.0}`); got.Outcome != healthResponseHealthy {
			t.Fatalf("mathematically integral number did not match: %#v", got)
		}
		if got := evaluateHealthResponseTest(assertion, 200, `{"code":true}`); got.Outcome != healthResponseIndeterminate {
			t.Fatalf("boolean was coerced to a number: %#v", got)
		}
	})
	t.Run("overlapping canonical code values invalidate assertion", func(t *testing.T) {
		assertion := healthNormalizedResponseAssertion{
			PayloadKind:             "json",
			AcceptedHTTPStatusCodes: []int{200},
			ProviderResultCodeMode:  "documented",
			ProviderResultCodePath:  healthNormalizedResponsePath{JSONPointer: "#/code"},
			ProviderResultCodeType:  "number",
			ProviderSuccessCodes:    []healthAssertionScalar{{ValueType: "number", Value: "1"}},
			ProviderErrorCodes:      []healthAssertionScalar{{ValueType: "number", Value: "1.0"}},
		}
		if err := validateHealthNormalizedResponseAssertion(assertion); err == nil {
			t.Fatal("numerically overlapping code sets passed validation")
		}
	})
}

func TestHealthNormalizedResponseAssertionBranches(t *testing.T) {
	assertion := healthNormalizedResponseAssertion{Branches: healthNormalizedJSONResponseBranchesFixture()}
	tests := []struct {
		name       string
		status     int
		body       string
		wantState  healthResponseAssertionOutcome
		wantReason string
		wantClass  string
	}{
		{name: "success object branch with valid empty collection", status: 200, body: `{"success":{"items":[]}}`, wantState: healthResponseHealthy, wantReason: "response_assertion_passed"},
		{name: "documented object error branch", status: 200, body: `{"error":{"message":"bad"}}`, wantState: healthResponseUnhealthy, wantReason: "response_provider_error", wantClass: "provider_failure"},
		{name: "two positive member selectors are ambiguous", status: 200, body: `{"success":{"items":[]},"error":{"message":"bad"}}`, wantState: healthResponseIndeterminate, wantReason: "response_branch_ambiguous"},
		{name: "no member selector match is indeterminate", status: 200, body: `{"other":{}}`, wantState: healthResponseIndeterminate, wantReason: "response_branch_unmatched"},
		{name: "malformed candidate payload is indeterminate", status: 200, body: `not-json`, wantState: healthResponseIndeterminate, wantReason: "response_payload_invalid"},
		{name: "missing success collection is indeterminate", status: 200, body: `{"success":{}}`, wantState: healthResponseIndeterminate, wantReason: "response_shape_mismatch"},
		{name: "unlisted HTTP 403 remains unhealthy with unknown cause", status: 403, body: `not-json`, wantState: healthResponseUnhealthy, wantReason: "response_status_not_accepted", wantClass: "provider_failure"},
		{name: "unlisted HTTP 500 remains unhealthy with unknown cause", status: 500, body: `not-json`, wantState: healthResponseUnhealthy, wantReason: "response_status_not_accepted", wantClass: "provider_failure"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := evaluateHealthNormalizedResponseAssertion(assertion, healthHTTPResponse{StatusCode: test.status, Body: []byte(test.body)})
			if result.Outcome != test.wantState || result.ReasonCode != test.wantReason || result.ProviderErrorClass != test.wantClass {
				t.Fatalf("got outcome=%q reason=%q class=%q, want %q/%q/%q", result.Outcome, result.ReasonCode, result.ProviderErrorClass, test.wantState, test.wantReason, test.wantClass)
			}
		})
	}
	t.Run("unlisted HTTP status gate precedes response-body ceiling", func(t *testing.T) {
		body := []byte(strings.Repeat("x", int(healthTransportMaxBytes)+1))
		result := evaluateHealthNormalizedResponseAssertion(assertion, healthHTTPResponse{StatusCode: 500, Body: body})
		if result.Outcome != healthResponseUnhealthy || result.ReasonCode != "response_status_not_accepted" {
			t.Fatalf("oversized error status lost its status classification: %#v", result)
		}
	})
	t.Run("documented HTTP error status stays unhealthy", func(t *testing.T) {
		branch := healthNormalizedResponseBranch{
			ID: "forbidden", Classification: "provider_error", PayloadKind: "json",
			AcceptedHTTPStatusCodes: []int{403}, RootKind: "object",
			Discriminators:         []healthNormalizedResponseDiscriminator{{Path: healthNormalizedResponsePath{JSONPointer: "#/error"}, Predicate: "node_type", ValueType: "object"}},
			RequiredFields:         []healthNormalizedResponseField{{Path: healthNormalizedResponsePath{JSONPointer: "#/error"}, ValueType: "object", MinimumCount: 1, MaximumCount: 1}},
			ProviderResultCodeMode: "none",
		}
		assertion := healthNormalizedResponseAssertion{Branches: []healthNormalizedResponseBranch{branch}}
		result := evaluateHealthNormalizedResponseAssertion(assertion, healthHTTPResponse{StatusCode: 403, Body: []byte(`{"error":{}}`)})
		if result.Outcome != healthResponseUnhealthy || result.ProviderErrorClass != "provider_failure" {
			t.Fatalf("explicit error status was lost: %#v", result)
		}
	})
	t.Run("array success and object error select by root kind", func(t *testing.T) {
		branches := []healthNormalizedResponseBranch{
			{
				ID: "array-success", Classification: "success", PayloadKind: "json",
				AcceptedHTTPStatusCodes: []int{200}, RootKind: "array", ProviderResultCodeMode: "none",
				RequiredFields:   []healthNormalizedResponseField{{Path: healthNormalizedResponsePath{JSONPointer: "#"}, ValueType: "array", MinimumCount: 1, MaximumCount: 1}},
				ResultCollection: &healthNormalizedResponseCollection{JSONPointer: "#", EmptySemantics: "valid"},
			},
			{
				ID: "object-error", Classification: "provider_error", PayloadKind: "json",
				AcceptedHTTPStatusCodes: []int{200}, RootKind: "object", ProviderResultCodeMode: "none",
				Discriminators: []healthNormalizedResponseDiscriminator{{Path: healthNormalizedResponsePath{JSONPointer: "#/RESULT"}, Predicate: "node_type", ValueType: "object"}},
				RequiredFields: []healthNormalizedResponseField{{Path: healthNormalizedResponsePath{JSONPointer: "#/RESULT"}, ValueType: "object", MinimumCount: 1, MaximumCount: 1}},
			},
		}
		assertion := healthNormalizedResponseAssertion{Branches: branches}
		if got := evaluateHealthResponseTest(assertion, 200, `[]`); got.Outcome != healthResponseHealthy {
			t.Fatalf("valid empty array-success response did not pass: %#v", got)
		}
		if got := evaluateHealthResponseTest(assertion, 200, `{"RESULT":{}}`); got.Outcome != healthResponseUnhealthy {
			t.Fatalf("object error branch was not selected: %#v", got)
		}
	})
	t.Run("service member success and RESULT member error are exact-one", func(t *testing.T) {
		branches := []healthNormalizedResponseBranch{
			{
				ID: "service-success", Classification: "success", PayloadKind: "json",
				AcceptedHTTPStatusCodes: []int{200}, RootKind: "object", ProviderResultCodeMode: "none",
				Discriminators:   []healthNormalizedResponseDiscriminator{{Path: healthNormalizedResponsePath{JSONPointer: "#/service"}, Predicate: "node_type", ValueType: "array"}},
				RequiredFields:   []healthNormalizedResponseField{{Path: healthNormalizedResponsePath{JSONPointer: "#/service"}, ValueType: "array", MinimumCount: 1, MaximumCount: 1}},
				ResultCollection: &healthNormalizedResponseCollection{JSONPointer: "#/service", EmptySemantics: "valid"},
			},
			{
				ID: "result-error", Classification: "provider_error", PayloadKind: "json",
				AcceptedHTTPStatusCodes: []int{200}, RootKind: "object", ProviderResultCodeMode: "none",
				Discriminators: []healthNormalizedResponseDiscriminator{{Path: healthNormalizedResponsePath{JSONPointer: "#/RESULT"}, Predicate: "node_type", ValueType: "object"}},
				RequiredFields: []healthNormalizedResponseField{{Path: healthNormalizedResponsePath{JSONPointer: "#/RESULT"}, ValueType: "object", MinimumCount: 1, MaximumCount: 1}},
			},
		}
		assertion := healthNormalizedResponseAssertion{Branches: branches}
		for _, test := range []struct {
			name, body string
			outcome    healthResponseAssertionOutcome
			reason     string
		}{
			{name: "empty service collection", body: `{"service":[]}`, outcome: healthResponseHealthy, reason: "response_assertion_passed"},
			{name: "RESULT error object", body: `{"RESULT":{"code":"E"}}`, outcome: healthResponseUnhealthy, reason: "response_provider_error"},
			{name: "both branch members", body: `{"service":[],"RESULT":{}}`, outcome: healthResponseIndeterminate, reason: "response_branch_ambiguous"},
			{name: "neither branch member", body: `{"other":[]}`, outcome: healthResponseIndeterminate, reason: "response_branch_unmatched"},
			{name: "wrong service member type", body: `{"service":{}}`, outcome: healthResponseIndeterminate, reason: "response_branch_unmatched"},
		} {
			t.Run(test.name, func(t *testing.T) {
				got := evaluateHealthResponseTest(assertion, 200, test.body)
				if got.Outcome != test.outcome || got.ReasonCode != test.reason {
					t.Fatalf("got %q/%q, want %q/%q", got.Outcome, got.ReasonCode, test.outcome, test.reason)
				}
			})
		}
	})
	t.Run("selected branch shape drift is indeterminate without fallback", func(t *testing.T) {
		branches := []healthNormalizedResponseBranch{
			{
				ID: "success", Classification: "success", PayloadKind: "json",
				AcceptedHTTPStatusCodes: []int{200}, RootKind: "object", ProviderResultCodeMode: "none",
				Discriminators:   []healthNormalizedResponseDiscriminator{{Path: healthNormalizedResponsePath{JSONPointer: "#/kind"}, Predicate: "equals_any", ValueType: "string", Values: []healthAssertionScalar{{ValueType: "string", Value: "success"}}}},
				RequiredFields:   []healthNormalizedResponseField{{Path: healthNormalizedResponsePath{JSONPointer: "#/service"}, ValueType: "array", MinimumCount: 1, MaximumCount: 1}},
				ResultCollection: &healthNormalizedResponseCollection{JSONPointer: "#/service", EmptySemantics: "valid"},
			},
			{
				ID: "error", Classification: "provider_error", PayloadKind: "json",
				AcceptedHTTPStatusCodes: []int{200}, RootKind: "object", ProviderResultCodeMode: "none",
				Discriminators: []healthNormalizedResponseDiscriminator{{Path: healthNormalizedResponsePath{JSONPointer: "#/kind"}, Predicate: "equals_any", ValueType: "string", Values: []healthAssertionScalar{{ValueType: "string", Value: "error"}}}},
				RequiredFields: []healthNormalizedResponseField{{Path: healthNormalizedResponsePath{JSONPointer: "#/RESULT"}, ValueType: "object", MinimumCount: 1, MaximumCount: 1}},
			},
		}
		assertion := healthNormalizedResponseAssertion{Branches: branches}
		for _, test := range []struct {
			name, body string
			reason     string
		}{
			{name: "selected branch missing required service", body: `{"kind":"success"}`, reason: "response_shape_mismatch"},
			{name: "missing discriminator", body: `{"service":[]}`, reason: "response_branch_unmatched"},
			{name: "wrong discriminator type", body: `{"kind":1,"service":[]}`, reason: "response_branch_unmatched"},
		} {
			t.Run(test.name, func(t *testing.T) {
				got := evaluateHealthResponseTest(assertion, 200, test.body)
				if got.Outcome != healthResponseIndeterminate || got.ReasonCode != test.reason {
					t.Fatalf("got %q/%q, want indeterminate/%q", got.Outcome, got.ReasonCode, test.reason)
				}
			})
		}
	})
	t.Run("known provider error code cannot bypass selected error shape", func(t *testing.T) {
		branch := healthNormalizedResponseBranch{
			ID: "error", Classification: "provider_error", PayloadKind: "json",
			AcceptedHTTPStatusCodes: []int{200}, RootKind: "object",
			ProviderResultCodeMode: "documented", ProviderResultCodePath: healthNormalizedResponsePath{JSONPointer: "#/code"},
			ProviderResultCodeType: "string", ProviderResultCodeValues: []healthAssertionScalar{{ValueType: "string", Value: "ERR"}},
			RequiredFields: []healthNormalizedResponseField{{Path: healthNormalizedResponsePath{JSONPointer: "#/message"}, ValueType: "string", MinimumCount: 1, MaximumCount: 1}},
		}
		got := evaluateHealthResponseTest(healthNormalizedResponseAssertion{Branches: []healthNormalizedResponseBranch{branch}}, 200, `{"code":"ERR"}`)
		if got.Outcome != healthResponseIndeterminate || got.ReasonCode != "response_shape_mismatch" {
			t.Fatalf("known error code bypassed its branch shape: %#v", got)
		}
	})
	t.Run("selected empty-collection semantics are applied after selection", func(t *testing.T) {
		branch := healthNormalizedJSONResponseBranchesFixture()[0]
		branch.ResultCollection.EmptySemantics = "invalid"
		got := evaluateHealthResponseTest(healthNormalizedResponseAssertion{Branches: []healthNormalizedResponseBranch{branch}}, 200, `{"success":{"items":[]}}`)
		if got.Outcome != healthResponseUnhealthy || got.ReasonCode != "response_collection_empty" {
			t.Fatalf("selected invalid empty collection was not unhealthy: %#v", got)
		}
	})
	t.Run("unknown provider code precedes empty-collection failure", func(t *testing.T) {
		branch := healthNormalizedResponseBranch{
			ID: "success", Classification: "success", PayloadKind: "json",
			AcceptedHTTPStatusCodes: []int{200}, RootKind: "object",
			ProviderResultCodeMode: "documented", ProviderResultCodePath: healthNormalizedResponsePath{JSONPointer: "#/code"},
			ProviderResultCodeType: "string", ProviderResultCodeValues: []healthAssertionScalar{{ValueType: "string", Value: "OK"}},
			ResultCollection: &healthNormalizedResponseCollection{JSONPointer: "#/items", EmptySemantics: "invalid"},
		}
		assertion := healthNormalizedResponseAssertion{Branches: []healthNormalizedResponseBranch{branch}}
		unknown := evaluateHealthResponseTest(assertion, 200, `{"code":"UNKNOWN","items":[]}`)
		if unknown.Outcome != healthResponseIndeterminate || unknown.ReasonCode != "response_provider_code_unknown" {
			t.Fatalf("unknown code was masked by empty collection semantics: %#v", unknown)
		}
		known := evaluateHealthResponseTest(assertion, 200, `{"code":"OK","items":[]}`)
		if known.Outcome != healthResponseUnhealthy || known.ReasonCode != "response_collection_empty" {
			t.Fatalf("recognized success code did not reach empty semantics: %#v", known)
		}
	})
	t.Run("required and forbidden member selectors separate branches", func(t *testing.T) {
		branches := []healthNormalizedResponseBranch{
			{
				ID: "success-without-error", Classification: "success", PayloadKind: "json", AcceptedHTTPStatusCodes: []int{200}, RootKind: "object",
				Discriminators:         []healthNormalizedResponseDiscriminator{{Path: healthNormalizedResponsePath{JSONPointer: "#/error"}, Predicate: "absent"}},
				RequiredFields:         []healthNormalizedResponseField{{Path: healthNormalizedResponsePath{JSONPointer: "#/items"}, ValueType: "array", MinimumCount: 1, MaximumCount: 1}},
				ProviderResultCodeMode: "none",
				ResultCollection:       &healthNormalizedResponseCollection{JSONPointer: "#/items", EmptySemantics: "valid"},
			},
			{
				ID: "error-present", Classification: "provider_error", PayloadKind: "json", AcceptedHTTPStatusCodes: []int{200}, RootKind: "object",
				Discriminators:         []healthNormalizedResponseDiscriminator{{Path: healthNormalizedResponsePath{JSONPointer: "#/error"}, Predicate: "node_type", ValueType: "object"}},
				RequiredFields:         []healthNormalizedResponseField{{Path: healthNormalizedResponsePath{JSONPointer: "#/error"}, ValueType: "object", MinimumCount: 1, MaximumCount: 1}},
				ProviderResultCodeMode: "none",
			},
		}
		assertion := healthNormalizedResponseAssertion{Branches: branches}
		if result := evaluateHealthResponseTest(assertion, 200, `{"items":[]}`); result.Outcome != healthResponseHealthy {
			t.Fatalf("forbidden member selector rejected success body: %#v", result)
		}
		if result := evaluateHealthResponseTest(assertion, 200, `{"error":{}}`); result.Outcome != healthResponseUnhealthy {
			t.Fatalf("required member selector missed error body: %#v", result)
		}
	})

	t.Run("source-mapped provider class is preserved", func(t *testing.T) {
		branches := []healthNormalizedResponseBranch{
			{
				ID: "auth-error", Classification: "provider_error", PayloadKind: "json",
				AcceptedHTTPStatusCodes: []int{200}, RootKind: "object",
				Discriminators: []healthNormalizedResponseDiscriminator{{
					Path: healthNormalizedResponsePath{JSONPointer: "#/kind"}, Predicate: "equals_any", ValueType: "string", Values: []healthAssertionScalar{{ValueType: "string", Value: "error"}},
				}},
				ProviderResultCodeMode: "documented", ProviderResultCodePath: healthNormalizedResponsePath{JSONPointer: "#/code"}, ProviderResultCodeType: "string",
				ProviderResultCodeValues: []healthAssertionScalar{{ValueType: "string", Value: "AUTH"}},
				ErrorClasses:             []healthNormalizedProviderErrorClass{{Category: "credential_rejected", Values: []healthAssertionScalar{{ValueType: "string", Value: "AUTH"}}}},
			},
		}
		result := evaluateHealthNormalizedResponseAssertion(healthNormalizedResponseAssertion{Branches: branches}, healthHTTPResponse{StatusCode: 200, Body: []byte(`{"kind":"error","code":"AUTH"}`)})
		if result.Outcome != healthResponseUnhealthy || result.ProviderErrorClass != "credential_rejected" {
			t.Fatalf("documented provider-error category was not preserved: %#v", result)
		}
		unknown := evaluateHealthNormalizedResponseAssertion(healthNormalizedResponseAssertion{Branches: branches}, healthHTTPResponse{StatusCode: 200, Body: []byte(`{"kind":"error","code":"OTHER"}`)})
		if unknown.Outcome != healthResponseIndeterminate || unknown.ReasonCode != "response_provider_code_unknown" {
			t.Fatalf("unknown error code was not indeterminate: %#v", unknown)
		}
	})
	t.Run("overlapping same-path exact selector values reject contract", func(t *testing.T) {
		branches := healthNormalizedJSONResponseBranchesFixture()
		branches[0].Discriminators = []healthNormalizedResponseDiscriminator{{Path: healthNormalizedResponsePath{JSONPointer: "#/state"}, Predicate: "equals_any", ValueType: "string", Values: []healthAssertionScalar{{ValueType: "string", Value: "x"}}}}
		branches[1].Discriminators = []healthNormalizedResponseDiscriminator{{Path: healthNormalizedResponsePath{JSONPointer: "#/state"}, Predicate: "equals_any", ValueType: "string", Values: []healthAssertionScalar{{ValueType: "string", Value: "x"}}}}
		if err := validateHealthNormalizedResponseBranches(branches); err == nil {
			t.Fatal("overlapping exact same-path selectors passed branch validation")
		}
	})
	t.Run("branch and selector ceilings are enforced", func(t *testing.T) {
		branch := healthNormalizedResponseBranch{
			ID: "branch", Classification: "success", PayloadKind: "json",
			AcceptedHTTPStatusCodes: []int{200}, RootKind: "object", ProviderResultCodeMode: "none",
		}
		tooMany := make([]healthNormalizedResponseBranch, 17)
		for index := range tooMany {
			tooMany[index] = branch
			tooMany[index].ID = fmt.Sprintf("branch-%d", index)
		}
		if err := validateHealthNormalizedResponseBranches(tooMany); err == nil {
			t.Fatal("17 response branches exceeded the contract ceiling")
		}
		for index := 0; index < 9; index++ {
			branch.Discriminators = append(branch.Discriminators, healthNormalizedResponseDiscriminator{
				Path: healthNormalizedResponsePath{JSONPointer: fmt.Sprintf("#/field-%d", index)}, Predicate: "present",
			})
		}
		if err := validateHealthNormalizedResponseBranches([]healthNormalizedResponseBranch{branch}); err == nil {
			t.Fatal("9 response discriminators exceeded the contract ceiling")
		}
	})
}

func TestHealthNormalizedXMLResponseAssertionBranches(t *testing.T) {
	const ns = "urn:branch:response"
	branches := []healthNormalizedResponseBranch{
		{
			ID: "success", Classification: "success", PayloadKind: "xml", AcceptedHTTPStatusCodes: []int{200},
			RootKind: "xml_element", RootQName: xml.Name{Space: ns, Local: "Envelope"},
			Discriminators:         []healthNormalizedResponseDiscriminator{{Path: healthNormalizedResponsePath{XMLPath: []xml.Name{{Space: ns, Local: "Envelope"}, {Space: ns, Local: "Data"}}}, Predicate: "node_type", ValueType: "object"}},
			RequiredFields:         []healthNormalizedResponseField{{Path: healthNormalizedResponsePath{XMLPath: []xml.Name{{Space: ns, Local: "Envelope"}, {Space: ns, Local: "Data"}}}, ValueType: "object", MinimumCount: 1, MaximumCount: 1}},
			ProviderResultCodeMode: "none",
			ResultCollection:       &healthNormalizedResponseCollection{XMLContainer: []xml.Name{{Space: ns, Local: "Envelope"}, {Space: ns, Local: "Data"}, {Space: ns, Local: "Items"}}, XMLItemPath: []xml.Name{{Space: ns, Local: "Item"}}, EmptySemantics: "valid"},
		},
		{
			ID: "error", Classification: "provider_error", PayloadKind: "xml", AcceptedHTTPStatusCodes: []int{200},
			RootKind: "xml_element", RootQName: xml.Name{Space: ns, Local: "Envelope"},
			Discriminators:         []healthNormalizedResponseDiscriminator{{Path: healthNormalizedResponsePath{XMLPath: []xml.Name{{Space: ns, Local: "Envelope"}, {Space: ns, Local: "Error"}}}, Predicate: "node_type", ValueType: "object"}},
			RequiredFields:         []healthNormalizedResponseField{{Path: healthNormalizedResponsePath{XMLPath: []xml.Name{{Space: ns, Local: "Envelope"}, {Space: ns, Local: "Error"}}}, ValueType: "object", MinimumCount: 1, MaximumCount: 1}},
			ProviderResultCodeMode: "none",
		},
	}
	assertion := healthNormalizedResponseAssertion{Branches: branches}
	t.Run("success branch container empty semantics", func(t *testing.T) {
		result := evaluateHealthResponseTest(assertion, 200, `<r:Envelope xmlns:r="`+ns+`"><r:Data><r:Items/></r:Data></r:Envelope>`)
		if result.Outcome != healthResponseHealthy {
			t.Fatalf("documented empty XML collection did not pass: %#v", result)
		}
	})
	t.Run("missing XML container is indeterminate", func(t *testing.T) {
		result := evaluateHealthResponseTest(assertion, 200, `<r:Envelope xmlns:r="`+ns+`"><r:Data><r:Other/></r:Data></r:Envelope>`)
		if result.Outcome != healthResponseIndeterminate || result.ReasonCode != "response_shape_mismatch" {
			t.Fatalf("missing XML container was not indeterminate: %#v", result)
		}
	})
	t.Run("same root error member selects provider error", func(t *testing.T) {
		result := evaluateHealthResponseTest(assertion, 200, `<r:Envelope xmlns:r="`+ns+`"><r:Error><r:Code>bad</r:Code></r:Error></r:Envelope>`)
		if result.Outcome != healthResponseUnhealthy || result.ProviderErrorClass != "provider_failure" {
			t.Fatalf("XML error shape was not classified generically: %#v", result)
		}
	})
	t.Run("both XML shapes are indeterminate", func(t *testing.T) {
		result := evaluateHealthResponseTest(assertion, 200, `<r:Envelope xmlns:r="`+ns+`"><r:Data><r:Items/></r:Data><r:Error><r:Code>bad</r:Code></r:Error></r:Envelope>`)
		if result.Outcome != healthResponseIndeterminate || result.ReasonCode != "response_branch_ambiguous" {
			t.Fatalf("coexisting XML shapes were not made indeterminate: %#v", result)
		}
	})
	t.Run("SOAP fault cannot bypass an unmatched branch selector", func(t *testing.T) {
		soapNS := "http://schemas.xmlsoap.org/soap/envelope/"
		branch := healthNormalizedResponseBranch{
			ID: "success", Classification: "success", PayloadKind: "soap_xml",
			AcceptedHTTPStatusCodes: []int{200}, RootKind: "xml_element", RootQName: xml.Name{Space: soapNS, Local: "Envelope"},
			ProviderResultCodeMode: "none",
			Discriminators:         []healthNormalizedResponseDiscriminator{{Path: healthNormalizedResponsePath{XMLPath: []xml.Name{{Space: soapNS, Local: "Envelope"}, {Space: soapNS, Local: "Body"}, {Local: "Success"}}}, Predicate: "present"}},
		}
		body := `<s:Envelope xmlns:s="` + soapNS + `"><s:Body><s:Fault/></s:Body></s:Envelope>`
		got := evaluateHealthNormalizedResponseAssertion(healthNormalizedResponseAssertion{Branches: []healthNormalizedResponseBranch{branch}}, healthHTTPResponse{StatusCode: 200, Body: []byte(body)})
		if got.Outcome != healthResponseIndeterminate || got.ReasonCode != "response_branch_unmatched" {
			t.Fatalf("SOAP fault bypassed branch selection: %#v", got)
		}
	})
}

func TestHealthNormalizedXMLResponseAssertionOutcomes(t *testing.T) {
	const prefix = `<r:Envelope xmlns:r="urn:example:response">`
	const suffix = `</r:Envelope>`
	tests := []struct {
		name      string
		middle    string
		wantState healthResponseAssertionOutcome
		wantCode  string
	}{
		{name: "present container with zero items is valid", middle: `<r:Code>OK</r:Code><r:Data>before<r:Items/>after</r:Data>`, wantState: healthResponseHealthy, wantCode: "response_assertion_passed"},
		{name: "present container with item is valid", middle: `<r:Code>OK</r:Code><r:Data><r:Items><r:Item>1</r:Item></r:Items></r:Data>`, wantState: healthResponseHealthy, wantCode: "response_assertion_passed"},
		{name: "missing container is indeterminate", middle: `<r:Code>OK</r:Code><r:Data/>`, wantState: healthResponseIndeterminate, wantCode: "response_shape_mismatch"},
		{name: "missing code is indeterminate", middle: `<r:Data><r:Items/></r:Data>`, wantState: healthResponseIndeterminate, wantCode: "response_provider_code_unknown"},
		{name: "documented error precedes success shape", middle: `<r:Code>ERROR</r:Code>`, wantState: healthResponseUnhealthy, wantCode: "response_provider_error"},
		{name: "XML scalar whitespace is not normalized", middle: `<r:Code> OK </r:Code><r:Data><r:Items/></r:Data>`, wantState: healthResponseIndeterminate, wantCode: "response_provider_code_unknown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := evaluateHealthResponseTest(healthXMLAssertionFixture(), 200, prefix+test.middle+suffix)
			if result.Outcome != test.wantState || result.ReasonCode != test.wantCode {
				t.Fatalf("got outcome=%q reason=%q, want %q/%q", result.Outcome, result.ReasonCode, test.wantState, test.wantCode)
			}
		})
	}
}

func TestHealthNormalizedXMLSOAPAndParserBounds(t *testing.T) {
	const soapNS = "http://www.w3.org/2003/05/soap-envelope"
	assertion := healthNormalizedResponseAssertion{
		PayloadKind:             "soap_xml",
		AcceptedHTTPStatusCodes: []int{200},
		SOAPEnvelopeNamespace:   soapNS,
		ProviderResultCodeMode:  "none",
	}
	for _, test := range []struct {
		name      string
		body      string
		wantState healthResponseAssertionOutcome
		wantCode  string
	}{
		{name: "fault is unhealthy", body: `<s:Envelope xmlns:s="` + soapNS + `"><s:Body><s:Fault/></s:Body></s:Envelope>`, wantState: healthResponseUnhealthy, wantCode: "response_provider_error"},
		{name: "missing SOAP body is indeterminate", body: `<s:Envelope xmlns:s="` + soapNS + `"><s:Header/></s:Envelope>`, wantState: healthResponseIndeterminate, wantCode: "response_payload_invalid"},
		{name: "wrong namespace is indeterminate", body: `<s:Envelope xmlns:s="urn:wrong"><s:Body/></s:Envelope>`, wantState: healthResponseIndeterminate, wantCode: "response_payload_invalid"},
		{name: "doctype is rejected", body: `<!DOCTYPE x [<!ENTITY y "z">]><x/>`, wantState: healthResponseIndeterminate, wantCode: "response_payload_invalid"},
		{name: "XML declaration is accepted", body: `<?xml version="1.0" encoding="UTF-8"?><s:Envelope xmlns:s="` + soapNS + `"><s:Body/></s:Envelope>`, wantState: healthResponseHealthy, wantCode: "response_assertion_passed"},
		{name: "multiple roots are rejected", body: `<a/><b/>`, wantState: healthResponseIndeterminate, wantCode: "response_payload_invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := evaluateHealthResponseTest(assertion, 200, test.body)
			if result.Outcome != test.wantState || result.ReasonCode != test.wantCode {
				t.Fatalf("got outcome=%q reason=%q, want %q/%q", result.Outcome, result.ReasonCode, test.wantState, test.wantCode)
			}
		})
	}
	t.Run("XML nesting ceiling is enforced", func(t *testing.T) {
		deep := strings.Repeat(`<x:a xmlns:x="urn:deep">`, healthOperationPlanMaxJSONDepth+1) + strings.Repeat(`</x:a>`, healthOperationPlanMaxJSONDepth+1)
		result := evaluateHealthResponseTest(assertion, 200, deep)
		if result.Outcome != healthResponseIndeterminate || result.ReasonCode != "response_payload_invalid" {
			t.Fatalf("deep XML was accepted: %#v", result)
		}
	})
	t.Run("XML node and token ceilings are enforced", func(t *testing.T) {
		body := `<x:Root xmlns:x="urn:wide">` + strings.Repeat(`<x:Leaf/>`, healthResponseXMLMaxNodes) + `</x:Root>`
		result := evaluateHealthResponseTest(assertion, 200, body)
		if result.Outcome != healthResponseIndeterminate || result.ReasonCode != "response_payload_invalid" {
			t.Fatalf("wide XML was accepted: %#v", result)
		}
	})
	t.Run("invalid UTF-8 XML is rejected", func(t *testing.T) {
		body := []byte(`<x:Envelope xmlns:x="` + soapNS + `"><x:Body><x:Fault>`)
		body = append(body, 0xff)
		body = append(body, []byte(`</x:Fault></x:Body></x:Envelope>`)...)
		result := evaluateHealthNormalizedResponseAssertion(assertion, healthHTTPResponse{StatusCode: 200, Body: body})
		if result.Outcome != healthResponseIndeterminate || result.ReasonCode != "response_payload_invalid" {
			t.Fatalf("invalid UTF-8 XML was accepted: %#v", result)
		}
	})
}

func TestHealthNormalizedResponseAssertionRejectsExponentAliases(t *testing.T) {
	assertion := healthNormalizedResponseAssertion{
		PayloadKind:             "json",
		AcceptedHTTPStatusCodes: []int{200},
		ProviderResultCodeMode:  "documented",
		ProviderResultCodePath:  healthNormalizedResponsePath{JSONPointer: "#/code"},
		ProviderResultCodeType:  "number",
		ProviderSuccessCodes:    []healthAssertionScalar{{ValueType: "number", Value: "1"}},
		ProviderErrorCodes:      []healthAssertionScalar{{ValueType: "number", Value: "2"}},
	}
	result := evaluateHealthResponseTest(assertion, 200, `{"code":1e999999999999999}`)
	if result.Outcome != healthResponseIndeterminate || result.ReasonCode != "response_provider_code_unknown" {
		t.Fatalf("oversized response exponent was not rejected: %#v", result)
	}
}

func TestHealthXMLScalarLexicalTypesAreStrict(t *testing.T) {
	tests := []struct {
		valueType string
		text      string
		want      bool
	}{
		{valueType: "boolean", text: "true", want: true},
		{valueType: "boolean", text: "0", want: true},
		{valueType: "boolean", text: "yes", want: false},
		{valueType: "integer", text: "+001", want: true},
		{valueType: "integer", text: "1.0", want: false},
		{valueType: "integer", text: "1e2", want: false},
		{valueType: "number", text: "-1.25e+3", want: true},
		{valueType: "number", text: "NaN", want: false},
		{valueType: "number", text: "INF", want: false},
		{valueType: "number", text: " 1 ", want: false},
	}
	for _, test := range tests {
		t.Run(test.valueType+"/"+test.text, func(t *testing.T) {
			document, err := decodeHealthBoundedResponseXML([]byte(`<x:v xmlns:x="urn:test">` + test.text + `</x:v>`))
			if err != nil {
				t.Fatal(err)
			}
			got := healthXMLNodeHasType(document, document.Root, test.valueType)
			if got != test.want {
				t.Fatalf("got %t, want %t", got, test.want)
			}
		})
	}
}
