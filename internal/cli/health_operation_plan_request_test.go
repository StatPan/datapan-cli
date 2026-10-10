package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const healthPlanSyntheticCredential = "local-synthetic-secret-never-in-receipt"

func readSyntheticHealthPlan(t *testing.T, name string) healthOperationPlanRecord {
	t.Helper()
	plan := readHealthOperationPlanFixture(t, name)
	// Request-construction tests exercise field-to-wire translation after
	// authorization. Use allowed authority labels in this in-memory test copy;
	// the source fixtures themselves remain marked test-only and are rejected
	// by the production loader and admission validator.
	plan.SourceBinding.TestOnly = false
	contract := plan.RequestPlan.RequestContract
	contract.Transport.Authority = "operation_document"
	contract.OperationEffect.Authority = "operation_document"
	if contract.Transport.Protocol == "SOAP" {
		contract.ResponseAssertion.Kind = "soap_fault_free"
	} else {
		contract.ResponseAssertion.Kind = "json_contract"
	}
	contract.ResponseAssertion.ExpectedStatusCodes = []int{http.StatusOK}
	assertionArtifactPath := healthOperationResponseAssertionArtifactPathPrefix + plan.OperationIdentity.OperationID + ".json"
	contract.ResponseAssertion.AssertionRef = assertionArtifactPath + "#/assertion"
	contract.ResponseAssertion.EvidenceRefs = []healthOperationPlanEvidenceRef{{
		ArtifactPath: assertionArtifactPath,
		JSONPointer:  "#/assertion",
		SHA256:       strings.Repeat("1", 64),
		EvidenceKind: "reviewed_policy",
	}}
	for i := range contract.Parameters {
		switch contract.Parameters[i].ValueStrategy.Kind {
		case "credential_reference":
			contract.Parameters[i].ValueStrategy.Authority = "runtime_binding"
		case "reviewed_enum", "reviewed_literal", "opaque_reviewed_value":
			contract.Parameters[i].ValueStrategy.Authority = "reviewed_policy"
		default:
			contract.Parameters[i].ValueStrategy.Authority = "operation_document"
		}
	}
	if err := validateHealthOperationPlanRecord(plan); err != nil {
		t.Fatalf("synthetic operation plan is invalid: %v", err)
	}
	return plan
}

func readHealthOperationPlanFixture(t *testing.T, name string) healthOperationPlanRecord {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "operation-observation-plan", name))
	if err != nil {
		t.Fatal(err)
	}
	var plan healthOperationPlanRecord
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	return plan
}

func setHealthPlanObservationOnlyAssertion(t *testing.T, plan *healthOperationPlanRecord) {
	t.Helper()
	response := &plan.RequestPlan.RequestContract.ResponseAssertion
	response.Kind = "observation_only"
	response.ExpectedStatusCodes = nil
	response.EmptyResultSemantics = "not_applicable"
	path := healthOperationResponseAssertionArtifactPathPrefix + plan.OperationIdentity.OperationID + ".json"
	response.AssertionRef = path + "#/assertion"
	response.EvidenceRefs = []healthOperationPlanEvidenceRef{{
		ArtifactPath: path,
		JSONPointer:  "#/assertion",
		SHA256:       strings.Repeat("a", 64),
		EvidenceKind: "reviewed_policy",
	}}
}

func healthPlanTestIntPointer(value int) *int { return &value }

type healthPlanCaptureClient struct {
	calls       int
	method      string
	url         *url.URL
	header      http.Header
	body        []byte
	status      int
	response    string
	readFailure bool
}

type healthPlanReadFailureBody struct{}

func (healthPlanReadFailureBody) Read([]byte) (int, error) {
	return 0, errors.New("synthetic response read failure")
}

func (c *healthPlanCaptureClient) Do(req *http.Request) (*http.Response, error) {
	c.calls++
	c.method = req.Method
	c.url = req.URL
	c.header = req.Header.Clone()
	if req.Body != nil {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		c.body = body
	}
	status := c.status
	if status == 0 {
		status = http.StatusOK
	}
	var responseBody io.ReadCloser = io.NopCloser(strings.NewReader(c.response))
	if c.readFailure {
		responseBody = io.NopCloser(healthPlanReadFailureBody{})
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"text/xml; charset=utf-8"}},
		Body:       responseBody,
		Request:    req,
	}, nil
}

func TestHealthOperationPlanRESTAndSOAPRequestTranslation(t *testing.T) {
	t.Run("explicit reviewed HEAD is preserved without creating a data response", func(t *testing.T) {
		plan := readSyntheticHealthPlan(t, "synthetic-rest-list.json")
		contract := plan.RequestPlan.RequestContract
		contract.Transport.HTTPMethod = http.MethodHead
		contract.OperationEffect.Authority = "reviewed_policy"
		contract.OperationEffect.EvidenceRefs = []healthOperationPlanEvidenceRef{
			{ArtifactPath: healthOperationPolicyArtifactPath, EvidenceKind: "reviewed_policy", JSONPointer: "#/profiles/0/effect_review", SHA256: strings.Repeat("a", 64)},
			{ArtifactPath: "reports/operation-document-evidence/synthetic.json", EvidenceKind: "operation_document", JSONPointer: "#/transport/http_method", SHA256: strings.Repeat("b", 64)},
			{ArtifactPath: "reports/operation-document-evidence/synthetic.json", EvidenceKind: "operation_document", JSONPointer: "#/operation_document/title", SHA256: strings.Repeat("b", 64)},
			{ArtifactPath: "reports/operation-document-evidence/synthetic.json", EvidenceKind: "operation_document", JSONPointer: "#/operation_document/purpose", SHA256: strings.Repeat("b", 64)},
		}
		shape, err := healthOperationPlanRequestShape(plan, healthPlanSyntheticCredential, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		request, err := buildHealthHTTPRequest(shape)
		if err != nil {
			t.Fatal(err)
		}
		if request.Method != http.MethodHead || request.Body != nil || request.ContentLength != 0 {
			t.Fatalf("plan method was not preserved as an empty-body HEAD: %#v", request)
		}
	})

	t.Run("REST query auth and single request", func(t *testing.T) {
		plan := readSyntheticHealthPlan(t, "synthetic-rest-list.json")
		shape, err := healthOperationPlanRequestShape(plan, healthPlanSyntheticCredential, time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if shape.Method != http.MethodGet || shape.Protocol != healthHTTPREST || !shape.PublicTargetOnly || shape.RequestBudget != 1 || shape.MaxRequestBytes != 4096 || shape.MaxResponseBytes != 16384 {
			t.Fatalf("unexpected REST request bounds: %#v", shape)
		}
		request, err := buildHealthHTTPRequest(shape)
		if err != nil {
			t.Fatal(err)
		}
		if !request.Close {
			t.Fatal("operation-plan request can reuse a connection")
		}
		client := &healthPlanCaptureClient{response: `{"items":[]}`}
		response, err := executeHealthHTTPRequest(context.Background(), client, shape)
		if err != nil {
			t.Fatal(err)
		}
		if client.calls != 1 || client.method != http.MethodGet || client.url == nil || client.url.Host != "api.example.invalid" || client.url.Path != "/v1/items" {
			t.Fatalf("REST request did not preserve the exact plan identity: calls=%d method=%s url=%v", client.calls, client.method, client.url)
		}
		if client.url.Query().Get("page") != "1" || client.url.Query().Get("serviceKey") != healthPlanSyntheticCredential || client.header.Get("Authorization") != "" {
			t.Fatalf("REST request did not use the reviewed query placement: query=%v headers=%v", client.url.Query(), client.header)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("REST response status changed: %d", response.StatusCode)
		}
	})

	t.Run("explicit endpoint port is preserved without default-port normalization", func(t *testing.T) {
		plan := readSyntheticHealthPlan(t, "synthetic-rest-list.json")
		port := 443
		plan.OperationIdentity.RegisteredEndpoint.Port = &port
		plan.RequestPlan.RequestContract.Transport.Port = &port
		shape, err := healthOperationPlanRequestShape(plan, healthPlanSyntheticCredential, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		request, err := buildHealthHTTPRequest(shape)
		if err != nil {
			t.Fatal(err)
		}
		if request.URL.Host != "api.example.invalid:443" || request.URL.String() != "https://api.example.invalid:443/v1/items?page=1&serviceKey="+url.QueryEscape(healthPlanSyntheticCredential) {
			t.Fatalf("explicit endpoint port was normalized or altered: %s", request.URL.String())
		}
	})

	t.Run("identity transport and explicit port must agree exactly", func(t *testing.T) {
		for _, test := range []struct {
			name          string
			identityPort  *int
			transportPort *int
			transportHost string
		}{
			{name: "mismatch", identityPort: healthPlanTestIntPointer(443), transportPort: healthPlanTestIntPointer(8443), transportHost: "api.example.invalid"},
			{name: "missing identity port", identityPort: nil, transportPort: healthPlanTestIntPointer(443), transportHost: "api.example.invalid"},
			{name: "missing request port", identityPort: healthPlanTestIntPointer(443), transportPort: nil, transportHost: "api.example.invalid"},
			{name: "embedded port without explicit field", identityPort: nil, transportPort: nil, transportHost: "api.example.invalid:443"},
			{name: "out of range", identityPort: healthPlanTestIntPointer(65536), transportPort: healthPlanTestIntPointer(65536), transportHost: "api.example.invalid"},
		} {
			t.Run(test.name, func(t *testing.T) {
				plan := readSyntheticHealthPlan(t, "synthetic-rest-list.json")
				plan.OperationIdentity.RegisteredEndpoint.Port = test.identityPort
				plan.RequestPlan.RequestContract.Transport.Port = test.transportPort
				plan.RequestPlan.RequestContract.Transport.Host = test.transportHost
				if err := validateHealthOperationPlanRecord(plan); err == nil {
					t.Fatal("endpoint or explicit port mismatch passed the pre-dispatch plan gate")
				}
			})
		}
	})

	t.Run("SOAP 1.1 header auth and document literal body", func(t *testing.T) {
		plan := readSyntheticHealthPlan(t, "synthetic-soap-read.json")
		shape, err := healthOperationPlanRequestShape(plan, healthPlanSyntheticCredential, time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		client := &healthPlanCaptureClient{response: `<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Header/><soap:Body><ReadResponse/></soap:Body></soap:Envelope>`}
		response, err := executeHealthHTTPRequest(context.Background(), client, shape)
		if err != nil {
			t.Fatal(err)
		}
		if client.calls != 1 || client.method != http.MethodPost || client.url == nil || client.url.Host != "soap.example.invalid" || client.url.Path != "/service" {
			t.Fatalf("SOAP request did not preserve the exact plan identity: calls=%d method=%s url=%v", client.calls, client.method, client.url)
		}
		if client.header.Get("SOAPAction") != "urn:synthetic:Read" || !strings.HasPrefix(client.header.Get("Content-Type"), "text/xml") || !bytes.Contains(client.body, []byte(`<op:Read xmlns:op="urn:synthetic:items">`)) || !bytes.Contains(client.body, []byte(`<p0:recordId`)) || !bytes.Contains(client.body, []byte(`<h0:ServiceKey xmlns:h0="urn:synthetic:auth">`+healthPlanSyntheticCredential)) {
			t.Fatalf("SOAP request did not preserve its explicit action, body, and header-auth placement: headers=%v body=%s", client.header, client.body)
		}
		passed, err := healthOperationPlanResponseAssertion(plan, response)
		if err != nil || !passed {
			t.Fatalf("SOAP fault-free assertion failed: passed=%t err=%v", passed, err)
		}
	})

	t.Run("SOAP 1.2 uses action media parameter", func(t *testing.T) {
		plan := readSyntheticHealthPlan(t, "synthetic-soap-read.json")
		contract := plan.RequestPlan.RequestContract
		contract.Transport.SOAPVersion = "1.2"
		contract.Transport.EnvelopeNamespace = "http://www.w3.org/2003/05/soap-envelope"
		shape, err := healthOperationPlanRequestShape(plan, healthPlanSyntheticCredential, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		request, err := buildHealthHTTPRequest(shape)
		if err != nil {
			t.Fatal(err)
		}
		mediaType, params, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/soap+xml" || params["action"] != "urn:synthetic:Read" || request.Header.Get("SOAPAction") != "" {
			t.Fatalf("SOAP 1.2 action placement was not preserved: mediaType=%s params=%v err=%v", mediaType, params, err)
		}
	})

	t.Run("unsupported assertion rejected before HTTP client", func(t *testing.T) {
		plan := readSyntheticHealthPlan(t, "synthetic-rest-list.json")
		plan.RequestPlan.RequestContract.ResponseAssertion.Kind = "response_rows"
		if err := validateHealthOperationPlanRecord(plan); err == nil {
			t.Fatal("unsupported response assertion passed the pre-dispatch plan gate")
		}
		if _, err := healthOperationPlanRequestShape(plan, healthPlanSyntheticCredential, time.Now().UTC()); err == nil {
			t.Fatal("unsupported response assertion produced a request shape")
		}
	})

	t.Run("credentialed plan requires TLS", func(t *testing.T) {
		plan := readSyntheticHealthPlan(t, "synthetic-rest-list.json")
		plan.RequestPlan.RequestContract.Transport.Scheme = "http"
		if err := validateHealthOperationPlanRecord(plan); err == nil {
			t.Fatal("credentialed plan over cleartext HTTP passed the pre-dispatch gate")
		}
		if _, err := healthOperationPlanRequestShape(plan, healthPlanSyntheticCredential, time.Now().UTC()); err == nil {
			t.Fatal("credentialed plan over cleartext HTTP produced a request shape")
		}
	})
}

func TestHealthOperationPlanObservationOnlyContractGate(t *testing.T) {
	valid := readSyntheticHealthPlan(t, "synthetic-rest-list.json")
	setHealthPlanObservationOnlyAssertion(t, &valid)
	if err := validateHealthOperationPlanRecord(valid); err != nil {
		t.Fatalf("valid observation-only contract was rejected: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*healthOperationPlanRecord)
	}{
		{"status predicates", func(plan *healthOperationPlanRecord) {
			plan.RequestPlan.RequestContract.ResponseAssertion.ExpectedStatusCodes = []int{http.StatusOK}
		}},
		{"empty semantics", func(plan *healthOperationPlanRecord) {
			plan.RequestPlan.RequestContract.ResponseAssertion.EmptyResultSemantics = "valid"
		}},
		{"cross-operation artifact pointer", func(plan *healthOperationPlanRecord) {
			plan.RequestPlan.RequestContract.ResponseAssertion.AssertionRef = "reports/operation-response-assertions/other.json#/assertion"
		}},
		{"missing artifact binding", func(plan *healthOperationPlanRecord) {
			plan.RequestPlan.RequestContract.ResponseAssertion.EvidenceRefs = nil
		}},
		{"duplicate artifact binding", func(plan *healthOperationPlanRecord) {
			ref := plan.RequestPlan.RequestContract.ResponseAssertion.EvidenceRefs[0]
			plan.RequestPlan.RequestContract.ResponseAssertion.EvidenceRefs = append(plan.RequestPlan.RequestContract.ResponseAssertion.EvidenceRefs, ref)
		}},
		{"invalid artifact digest", func(plan *healthOperationPlanRecord) {
			plan.RequestPlan.RequestContract.ResponseAssertion.EvidenceRefs[0].SHA256 = "not-a-digest"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := readSyntheticHealthPlan(t, "synthetic-rest-list.json")
			setHealthPlanObservationOnlyAssertion(t, &plan)
			test.mutate(&plan)
			if err := validateHealthOperationPlanRecord(plan); err == nil {
				t.Fatal("invalid observation-only response contract passed pre-dispatch validation")
			}
		})
	}
}

func TestHealthOperationPlanRejectsSyntheticAuthorityPromotion(t *testing.T) {
	t.Run("transport", func(t *testing.T) {
		plan := readSyntheticHealthPlan(t, "synthetic-rest-list.json")
		plan.RequestPlan.RequestContract.Transport.Authority = "synthetic_fixture"
		if err := validateHealthOperationPlanRecord(plan); err == nil {
			t.Fatal("synthetic transport authority passed production admission")
		}
		if _, err := healthOperationPlanRequestShape(plan, "", time.Now().UTC()); err == nil {
			t.Fatal("synthetic transport authority produced a request shape")
		}
	})

	t.Run("operation effect", func(t *testing.T) {
		plan := readSyntheticHealthPlan(t, "synthetic-rest-list.json")
		plan.RequestPlan.RequestContract.OperationEffect.Authority = "synthetic_fixture"
		if err := validateHealthOperationPlanRecord(plan); err == nil {
			t.Fatal("synthetic operation-effect authority passed production admission")
		}
	})

	t.Run("parameter value", func(t *testing.T) {
		plan := readSyntheticHealthPlan(t, "synthetic-rest-list.json")
		if len(plan.RequestPlan.RequestContract.Parameters) == 0 {
			t.Fatal("synthetic REST fixture has no parameter to test")
		}
		plan.RequestPlan.RequestContract.Parameters[0].ValueStrategy.Authority = "synthetic_fixture"
		if err := validateHealthOperationPlanRecord(plan); err == nil {
			t.Fatal("synthetic parameter-value authority passed production admission")
		}
	})
}

func TestHealthPlanCredentialResolverRequiresExactLocalBinding(t *testing.T) {
	plan := readSyntheticHealthPlan(t, "synthetic-rest-list.json")
	config := healthCredentialBindingsConfig{
		SchemaVersion: healthCredentialBindingsSchemaVersion,
		Bindings: []healthCredentialBinding{{
			CredentialReference: plan.RuntimeBinding.CredentialReference,
			CredentialScopeKey:  plan.RuntimeBinding.CredentialScopeKey,
			Provider:            plan.SourceBinding.Provider,
			AdapterID:           plan.SourceBinding.AdapterID,
			AuthKind:            plan.RequestPlan.RequestContract.Authentication.Mechanism,
			CredentialGroupID:   "synthetic_local",
			CredentialEnvName:   "SYNTHETIC_API_KEY",
		}},
	}
	group := func(id string) (credentialGroup, bool) {
		if id != "synthetic_local" {
			return credentialGroup{}, false
		}
		return credentialGroup{ID: id, Provider: plan.SourceBinding.Provider, EnvNames: []string{"SYNTHETIC_API_KEY"}}, true
	}
	lookup := func(name string) (string, bool) {
		return healthPlanSyntheticCredential, name == "SYNTHETIC_API_KEY"
	}
	credential, err := resolveHealthPlanCredential(plan, config, group, lookup)
	if err != nil || credential.Value != healthPlanSyntheticCredential {
		t.Fatalf("exact synthetic binding did not resolve: credential=%#v err=%v", credential, err)
	}

	for _, mutate := range []func(*healthCredentialBinding){
		func(binding *healthCredentialBinding) { binding.CredentialReference += "/wrong" },
		func(binding *healthCredentialBinding) { binding.CredentialScopeKey += ".wrong" },
		func(binding *healthCredentialBinding) { binding.Provider += " wrong" },
		func(binding *healthCredentialBinding) { binding.AdapterID += "-wrong" },
		func(binding *healthCredentialBinding) { binding.AuthKind = "api_key" },
		func(binding *healthCredentialBinding) { binding.CredentialEnvName = "UNLISTED_SECRET" },
	} {
		wrong := config
		wrong.Bindings = append([]healthCredentialBinding(nil), config.Bindings...)
		mutate(&wrong.Bindings[0])
		_, err := resolveHealthPlanCredential(plan, wrong, group, lookup)
		if !errors.Is(err, errHealthCredentialBindingMismatch) || strings.Contains(err.Error(), healthPlanSyntheticCredential) {
			t.Fatalf("mismatched local binding did not fail closed with a redacted error: %v", err)
		}
	}
	_, err = resolveHealthPlanCredential(plan, config, group, func(string) (string, bool) { return "", false })
	if !errors.Is(err, errHealthCredentialSourceUnavailable) || strings.Contains(err.Error(), healthPlanSyntheticCredential) {
		t.Fatalf("missing env source did not fail closed with a redacted error: %v", err)
	}
}

func TestHealthCredentialBindingPreflightBoundsEntriesBeforeDecode(t *testing.T) {
	validPrefix := `{"schema_version":"datapan.health-credential-bindings.v1","bindings":[`
	var bomb strings.Builder
	bomb.WriteString(validPrefix)
	for index := 0; index <= healthCredentialBindingsMaxEntries; index++ {
		if index > 0 {
			bomb.WriteByte(',')
		}
		bomb.WriteString("null")
	}
	bomb.WriteString("]}")
	if err := preflightHealthCredentialBindingsJSON([]byte(bomb.String())); err == nil {
		t.Fatal("credential binding preflight accepted an entry-count allocation bomb")
	}
	if err := preflightHealthCredentialBindingsJSON([]byte(`{"schema_version":"x","bindings":[],"BINDINGS":[]}`)); err == nil {
		t.Fatal("credential binding preflight accepted a case-folded duplicate field")
	}
}

func TestHealthOperationPlanProbeReceiptIsBoundRedactedAndAtomic(t *testing.T) {
	loaded := healthOperationPlanLoadResult{
		Options:        healthOperationPlanOptions{AttemptID: "17e1fa72-eaf4-493a-9d97-d3fd3bc52a3c", RegistryRevision: strings.Repeat("a", 40), SourceID: "synthetic-source"},
		RegistryTrust:  registryTrustContext{DatasetID: "StatPan/datapan-registry", DatasetRevision: strings.Repeat("b", 40), Distribution: "huggingface_dataset", RegistrySHA256: strings.Repeat("c", 64)},
		ManifestSHA256: strings.Repeat("c", 64),
		IndexSHA256:    strings.Repeat("d", 64),
		Index: healthOperationPlanIndex{GenerationInputs: struct {
			GeneratorPath         string                           `json:"generator_path"`
			GeneratorSHA256       string                           `json:"generator_sha256"`
			OperationManifest     healthOperationPlanArtifactRef   `json:"operation_manifest"`
			OperationDenominators []healthOperationPlanArtifactRef `json:"operation_denominators"`
			LegacyPolicy          healthOperationPlanArtifactRef   `json:"legacy_policy"`
			ProviderIndex         healthOperationPlanArtifactRef   `json:"provider_index"`
			DocumentEvidence      []healthOperationPlanArtifactRef `json:"document_evidence,omitempty"`
		}{OperationManifest: healthOperationPlanArtifactRef{SHA256: strings.Repeat("e", 64)}, ProviderIndex: healthOperationPlanArtifactRef{SHA256: strings.Repeat("f", 64)}}},
		Shard:       healthOperationPlanShardRef{SHA256: strings.Repeat("1", 64)},
		SourceScope: healthOperationPlanSourceScope{IdentitySetSHA256: strings.Repeat("2", 64)},
		Plan: healthOperationPlanRecord{
			SourceBinding:     healthOperationPlanSourceBinding{SourceID: "synthetic-source", Provider: "synthetic-provider", AdapterID: "synthetic-adapter"},
			OperationIdentity: healthOperationPlanIdentity{OperationID: "synthetic-operation", Protocol: "REST"},
			RequestPlan:       healthOperationPlanRequestPlan{RequestContract: &healthOperationPlanRequestContract{}},
		},
	}
	loaded.Plan.RequestPlan.RequestContract.ResponseAssertion.Kind = "http_status"
	loaded.Plan.RequestPlan.RequestContract.Limits.TimeoutMS = 1000
	receipt := newHealthOperationPlanProbeReceipt(loaded, strings.Repeat("3", 64))
	if receipt.Operation.OperationID != loaded.Plan.OperationIdentity.OperationID || receipt.Operation.SourceID != loaded.Options.SourceID {
		t.Fatalf("receipt lost the selected source/operation binding: operation=%+v options=%+v", receipt.Operation, loaded.Options)
	}
	receipt.Execution.RequestStarted = true
	receipt.Execution.RequestBudget = 1
	receipt.Execution.DurationMS = 4
	receipt.Observation.ResponseObserved = true
	receipt.Observation.ObservedAt = "2026-10-07T00:00:00Z"
	receipt.Observation.HTTPStatus = http.StatusOK
	receipt.Observation.Outcome = "healthy"
	receipt.Observation.ReasonCode = "response_assertion_passed"
	receipt.Observation.AssertionStatus = "passed"

	data, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compileTestJSONSchema("../../schemas/datapan.health-operation-plan-probe.v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := compiled.Validate(instance); err != nil {
		t.Fatalf("receipt does not conform to its schema: %v", err)
	}
	for _, test := range []struct {
		name      string
		status    int
		outcome   string
		reason    string
		kind      string
		assertion string
	}{
		{"observation-only success status", http.StatusOK, "indeterminate", "response_semantics_unestablished", "observation_only", "not_run"},
		{"observation-only non-2xx", http.StatusInternalServerError, "unhealthy", "response_http_failure", "observation_only", "failed"},
	} {
		candidate := receipt
		candidate.Observation.HTTPStatus = test.status
		candidate.Observation.Outcome = test.outcome
		candidate.Observation.ReasonCode = test.reason
		candidate.Observation.AssertionKind = test.kind
		candidate.Observation.AssertionStatus = test.assertion
		candidateData, err := json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		candidateInstance, err := jsonschema.UnmarshalJSON(bytes.NewReader(candidateData))
		if err != nil {
			t.Fatal(err)
		}
		if err := compiled.Validate(candidateInstance); err != nil {
			t.Fatalf("%s receipt does not conform to its schema: %v", test.name, err)
		}
	}
	for _, test := range []struct {
		name   string
		mutate func(*healthOperationPlanProbeReceipt)
	}{
		{"missing status", func(candidate *healthOperationPlanProbeReceipt) { candidate.Observation.HTTPStatus = 0 }},
		{"response not observed", func(candidate *healthOperationPlanProbeReceipt) { candidate.Observation.ResponseObserved = false }},
		{"wrong success range", func(candidate *healthOperationPlanProbeReceipt) {
			candidate.Observation.HTTPStatus = http.StatusInternalServerError
		}},
		{"wrong assertion kind", func(candidate *healthOperationPlanProbeReceipt) {
			candidate.Observation.AssertionKind = "json_contract"
		}},
		{"wrong assertion status", func(candidate *healthOperationPlanProbeReceipt) { candidate.Observation.AssertionStatus = "passed" }},
		{"observation-only cannot be healthy", func(candidate *healthOperationPlanProbeReceipt) { candidate.Observation.Outcome = "healthy" }},
		{"observed result requires request start", func(candidate *healthOperationPlanProbeReceipt) { candidate.Execution.RequestStarted = false }},
		{"observed result requires one request budget", func(candidate *healthOperationPlanProbeReceipt) { candidate.Execution.RequestBudget = 0 }},
	} {
		candidate := receipt
		candidate.Observation.Outcome = "indeterminate"
		candidate.Observation.ReasonCode = "response_semantics_unestablished"
		candidate.Observation.AssertionKind = "observation_only"
		candidate.Observation.AssertionStatus = "not_run"
		test.mutate(&candidate)
		candidateData, err := json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		candidateInstance, err := jsonschema.UnmarshalJSON(bytes.NewReader(candidateData))
		if err != nil {
			t.Fatal(err)
		}
		if err := compiled.Validate(candidateInstance); err == nil {
			t.Fatalf("invalid observation-only receipt passed schema: %s", test.name)
		}
	}
	var identity map[string]any
	if err := json.Unmarshal(data, &identity); err != nil {
		t.Fatal(err)
	}
	registry := identity["registry"].(map[string]any)
	if registry["registry_revision"] != strings.Repeat("a", 40) || registry["distribution_dataset_revision"] != strings.Repeat("b", 40) || registry["registry_revision"] == registry["distribution_dataset_revision"] {
		t.Fatalf("receipt conflated source Git and distribution revisions: %s", data)
	}
	if _, ambiguous := registry["revision"]; ambiguous {
		t.Fatalf("receipt retained an ambiguous revision field: %s", data)
	}
	if bytes.Contains(data, []byte(healthPlanSyntheticCredential)) || bytes.Contains(data, []byte("serviceKey")) || bytes.Contains(data, []byte("api.example.invalid")) {
		t.Fatalf("receipt contains request or credential material: %s", data)
	}

	output := filepath.Join(t.TempDir(), "receipt.json")
	var stdout bytes.Buffer
	a := app{stdout: &stdout}
	if code := a.writeHealthOperationPlanReceipt(output, receipt); code != exitOK {
		t.Fatalf("receipt write returned %d", code)
	}
	fileBytes, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fileBytes, stdout.Bytes()) || !bytes.Equal(fileBytes, append(data, '\n')) {
		t.Fatal("stdout and atomic receipt file differ")
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("receipt file mode is %o, want 0600", info.Mode().Perm())
	}
}

func TestHealthOperationPlanProbeReceiptPreservesSemanticAssertionKind(t *testing.T) {
	loaded := healthOperationPlanLoadResult{
		Options:       healthOperationPlanOptions{AttemptID: "17e1fa72-eaf4-493a-9d97-d3fd3bc52a3c", RegistryRevision: strings.Repeat("a", 40)},
		RegistryTrust: registryTrustContext{DatasetID: "StatPan/datapan-registry", Distribution: "huggingface_dataset", DatasetRevision: strings.Repeat("b", 40)},
		Plan: healthOperationPlanRecord{
			RequestPlan: healthOperationPlanRequestPlan{RequestContract: &healthOperationPlanRequestContract{}},
		},
	}
	loaded.Plan.RequestPlan.RequestContract.ResponseAssertion.Kind = "json_contract"
	receipt := newHealthOperationPlanProbeReceipt(loaded, strings.Repeat("c", 64))
	if receipt.Observation.AssertionKind != "json_contract" {
		t.Fatalf("semantic assertion kind was downgraded in receipt: %q", receipt.Observation.AssertionKind)
	}
}

func TestHealthOperationPlanOversizedResponseIsIndeterminate(t *testing.T) {
	plan := readSyntheticHealthPlan(t, "synthetic-rest-list.json")
	plan.RequestPlan.RequestContract.Limits.MaxResponseBytes = 4
	shape, err := healthOperationPlanRequestShape(plan, healthPlanSyntheticCredential, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	shape.MaxResponseBytes = 4
	loaded := healthOperationPlanLoadResult{
		Options: healthOperationPlanOptions{
			AttemptID:        "17e1fa72-eaf4-493a-9d97-d3fd3bc52a3c",
			RegistryRevision: strings.Repeat("a", 40),
			Deadline:         time.Now().Add(5 * time.Second),
		},
		Plan: plan,
	}
	client := &healthPlanCaptureClient{response: "12345"}
	var stdout bytes.Buffer
	a := app{http: client, stdout: &stdout, healthOperationPlan: &loaded}
	receipt := newHealthOperationPlanProbeReceipt(loaded, strings.Repeat("3", 64))
	output := filepath.Join(t.TempDir(), "receipt.json")
	if code := a.executeHealthOperationPlanRequest(output, receipt, shape, time.Now()); code != exitRequest {
		t.Fatalf("oversized response execution returned %d, want non-healthy receipt exit %d", code, exitRequest)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var got healthOperationPlanProbeReceipt
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if client.calls != 1 || got.Observation.Outcome != "indeterminate" || got.Observation.ReasonCode != "response_limit_exceeded" || got.Observation.AssertionStatus != "not_run" {
		t.Fatalf("oversized response was misclassified: calls=%d receipt=%#v", client.calls, got)
	}
	if !got.Observation.ResponseObserved || got.Observation.HTTPStatus != http.StatusOK || got.Observation.ObservedAt == "" {
		t.Fatalf("bounded response receipt lost the observed HTTP response metadata: %#v", got.Observation)
	}
}

func TestHealthOperationPlanObservationOnlyReceiptSemantics(t *testing.T) {
	for _, test := range []struct {
		name        string
		status      int
		body        string
		maxBytes    int64
		outcome     string
		reason      string
		assertion   string
		readFailure bool
	}{
		{"HTTP 200 remains unknown", http.StatusOK, `{"items":[{"secret":"do-not-publish"}]}`, 4096, "indeterminate", "response_semantics_unestablished", "not_run", false},
		{"HTTP 204 remains unknown", http.StatusNoContent, "", 4096, "indeterminate", "response_semantics_unestablished", "not_run", false},
		{"HTTP 500 is an HTTP failure", http.StatusInternalServerError, `{"message":"do-not-publish"}`, 4096, "unhealthy", "response_http_failure", "failed", false},
		{"HTTP 500 outranks body cap", http.StatusInternalServerError, "too many bytes", 4, "unhealthy", "response_http_failure", "failed", false},
		{"HTTP 500 outranks body read error", http.StatusInternalServerError, "ignored", 4096, "unhealthy", "response_http_failure", "failed", true},
		{"response cap remains indeterminate", http.StatusOK, "too many bytes", 4, "indeterminate", "response_limit_exceeded", "not_run", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := readSyntheticHealthPlan(t, "synthetic-rest-list.json")
			setHealthPlanObservationOnlyAssertion(t, &plan)
			plan.RequestPlan.RequestContract.Limits.MaxResponseBytes = test.maxBytes
			if err := validateHealthOperationPlanRecord(plan); err != nil {
				t.Fatalf("synthetic observation-only plan is invalid: %v", err)
			}
			shape, err := healthOperationPlanRequestShape(plan, healthPlanSyntheticCredential, time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			loaded := healthOperationPlanLoadResult{
				Options: healthOperationPlanOptions{
					AttemptID:        "17e1fa72-eaf4-493a-9d97-d3fd3bc52a3c",
					RegistryRevision: strings.Repeat("a", 40),
					Deadline:         time.Now().Add(5 * time.Second),
				},
				Plan:              plan,
				ResponseAssertion: healthNormalizedResponseAssertion{ObservationOnly: true},
			}
			client := &healthPlanCaptureClient{status: test.status, response: test.body, readFailure: test.readFailure}
			var stdout bytes.Buffer
			a := app{http: client, stdout: &stdout, healthOperationPlan: &loaded}
			receipt := newHealthOperationPlanProbeReceipt(loaded, strings.Repeat("3", 64))
			output := filepath.Join(t.TempDir(), "receipt.json")
			if code := a.executeHealthOperationPlanRequest(output, receipt, shape, time.Now()); code != exitRequest {
				t.Fatalf("observation-only request returned %d, want exit %d", code, exitRequest)
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			var got healthOperationPlanProbeReceipt
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if client.calls != 1 || !got.Execution.RequestStarted || got.Execution.RequestBudget != 1 || !got.Observation.ResponseObserved || got.Observation.HTTPStatus != test.status || got.Observation.Outcome != test.outcome || got.Observation.ReasonCode != test.reason || got.Observation.AssertionStatus != test.assertion || got.Observation.AssertionKind != "observation_only" {
				t.Fatalf("request or receipt semantics differ: calls=%d receipt=%#v", client.calls, got)
			}
			if !bytes.Equal(data, stdout.Bytes()) {
				t.Fatal("stdout and durable receipt differ")
			}
			if test.body != "" && bytes.Contains(data, []byte(test.body)) || bytes.Contains(data, []byte(healthPlanSyntheticCredential)) || bytes.Contains(data, []byte("api.example.invalid")) {
				t.Fatalf("receipt leaked response/request material: %s", data)
			}
		})
	}
}

func compileTestJSONSchema(path string) (*jsonschema.Schema, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("https://schemas.datapan.dev/datapan.health-operation-plan-probe.v1.schema.json", document); err != nil {
		return nil, err
	}
	return compiler.Compile("https://schemas.datapan.dev/datapan.health-operation-plan-probe.v1.schema.json")
}

var _ HTTPClient = (*healthPlanCaptureClient)(nil)
