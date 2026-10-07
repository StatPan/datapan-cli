package cli

import (
	"encoding/xml"
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
		{name: "duplicate keys are rejected", body: `{"code":"OK","CODE":"ERROR","data":{"items":[]}}`, wantState: healthResponseIndeterminate, wantCode: "response_payload_invalid"},
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
		{name: "fault is unhealthy", body: `<s:Envelope xmlns:s="` + soapNS + `"><s:Body><s:Fault/></s:Body></s:Envelope>`, wantState: healthResponseUnhealthy, wantCode: "response_provider_fault"},
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
