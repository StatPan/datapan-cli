package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/StatPan/datapan-cli/internal/datago"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func diagnosticTestPlan() requestPlan {
	return requestPlan{
		Spec:      datago.Spec{ID: "15095335", Provider: "data.go.kr"},
		Operation: datago.Operation{Name: "getPolicyNews", Endpoint: "https://apis.data.go.kr/1371000/policyNewsService/getPolicyNewsList"},
	}
}

func TestReviewedDiagnosticFixturesRemainByteExactAndSchemaValid(t *testing.T) {
	digests := map[string]string{
		"approval-required.json":    "d11fc4e18aee6fe1a7f5c9c0a94a1e6e1bae0177447f2f6bcc0dddbe6961e7d3",
		"approval-propagating.json": "fe000f4082f948d6a96f045d7fae91c6bdf7288c6746196a8c7b0868d6416099",
		"credential-invalid.json":   "c5796d7bf59c6f282f9f75b14717a51ff716859e06f6325e784c48d507816497",
		"invalid-input.json":        "80adb4fce6ede5c34223468bf26b69e90c20c92d74661777c458a5238ad6ab07",
		"rate-limited.json":         "8be4fb69e91ae42c2a03510458b9b9fd23cf4780d551ea1ddaa505c8bc40d318",
		"provider-outage.json":      "33c0160c4cf136b34dc3befa1ff5803c71f3c37d7946fa2b56e70c69d4be6200",
		"contract-drift.json":       "13bc8af8c6b1540ef91a49f60ed9aab5514fabee3316868dc9f946adbe1da470",
		"semantic-quality.json":     "06fcc308aa039861b38f6da2fee8f23150ea7d2cac998b5d9e05b011ca1ca9b0",
		"stale-data.json":           "f4cc2d0f34bdfb74bbed9a3bcb5cad7b0f5444fa5ded6ca1742c916799aecf92",
		"ready.json":                "7ae2306176bdd007ca2a4ca822240e4e515c76967b2af0c816584463aee420fc",
		"unknown.json":              "e0635cf4980438141007607c66eca821383f605393a69bdb03522b3873c1dcf0",
	}
	root := filepath.Join("testdata", "diagnostic-envelope")
	schemaBytes, err := os.ReadFile(filepath.Join(root, "schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(schemaBytes)); got != diagnosticSchemaSHA256 {
		t.Fatalf("schema digest=%s", got)
	}
	mappingBytes, err := os.ReadFile(filepath.Join(root, "mapping.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(mappingBytes)); got != diagnosticMappingSHA256 {
		t.Fatalf("mapping digest=%s", got)
	}
	var metadata struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(schemaBytes, &metadata); err != nil {
		t.Fatal(err)
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaBytes))
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(metadata.ID, document); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	for name, digest := range digests {
		data, err := os.ReadFile(filepath.Join(root, "fixtures", name))
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != digest {
			t.Fatalf("%s digest=%s", name, got)
		}
		instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if err := schema.Validate(instance); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

// These seven cases cross the real command boundary. Helper-only assembly is
// insufficient evidence for the product journey because it can bypass
// discovery, provider execution, serialization, retry, and export behavior.
func TestDiagnosticDiscoveryToReusableExportJourneys(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		wantCode   string
		wantAction string
	}{
		{"bad request", 400, "invalid_input", "verify_request_parameters"},
		{"unauthorized remains unknown", 401, "unknown", "gather_more_evidence"},
		{"forbidden remains unknown", 403, "unknown", "gather_more_evidence"},
		{"not found remains unknown", 404, "unknown", "gather_more_evidence"},
		{"rate limited", 429, "rate_limited", "retry_with_backoff"},
		{"provider unavailable", 503, "provider_outage", "check_provider_status"},
		{"provider gateway failure", 502, "provider_outage", "check_provider_status"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, args := range [][]string{{"search", "기상", "--json"}, {"show", "15084084", "--json"}} {
				code, stdout, stderr := runTest(args, nil, nil)
				if code != exitOK || stderr != "" || !strings.Contains(stdout, `"15084084"`) {
					t.Fatalf("discovery args=%v code=%d stdout=%s stderr=%s", args, code, stdout, stderr)
				}
			}

			journeyStartedAt := time.Now().UTC().Add(-time.Second)
			journeyStart := journeyStartedAt.Format(time.RFC3339Nano)
			failureClient := roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: http.NoBody}, nil
			})
			failureArgs := []string{"get", "15084084", "--json", "--journey-started-at", journeyStart, "base_date=20260622", "base_time=0500"}
			code, failureJSON, stderr := runTest(failureArgs, fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "journey-secret"}, failureClient)
			if code != exitRequest || stderr != "" {
				t.Fatalf("failure code=%d stdout=%s stderr=%s", code, failureJSON, stderr)
			}
			var failed struct {
				Failure struct {
					Diagnostic *localDiagnosticOutcome `json:"diagnostic"`
				} `json:"failure"`
			}
			if err := json.Unmarshal([]byte(failureJSON), &failed); err != nil {
				t.Fatal(err)
			}
			diagnostic := failed.Failure.Diagnostic
			if diagnostic == nil || diagnostic.ConsumerHandoff == nil || diagnostic.ConsumerHandoff.Result.Code != tc.wantCode || len(diagnostic.ConsumerHandoff.Result.Recommended) != 1 || diagnostic.ConsumerHandoff.Result.Recommended[0].ActionID != tc.wantAction {
				t.Fatalf("runtime diagnosis/next action = %#v", diagnostic)
			}
			if diagnostic.ConsumerHandoff.Metrics == nil || diagnostic.ConsumerHandoff.Metrics.TimeToDiagnosisMS == nil || diagnostic.ConsumerHandoff.Metrics.TimeToFirstSuccessMS != nil || strings.Contains(failureJSON, "journey-secret") {
				t.Fatalf("failure journey metrics/redaction = %#v output=%s", diagnostic.ConsumerHandoff.Metrics, failureJSON)
			}
			diagnosedAt := diagnostic.Timing.DiagnosisComputedAt

			successClient := roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"rows":[{"value":1}]}`))}, nil
			})
			journeyFlags := []string{"--journey-started-at", journeyStart, "--journey-diagnosed-at", diagnosedAt}
			successArgs := append([]string{"get", "15084084", "--json"}, journeyFlags...)
			successArgs = append(successArgs, "base_date=20260622", "base_time=0500")
			code, successJSON, stderr := runTest(successArgs, fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "journey-secret"}, successClient)
			if code != exitOK || stderr != "" || !strings.Contains(successJSON, `"time_to_diagnosis_ms":`) || !strings.Contains(successJSON, `"time_to_first_success_ms":`) || strings.Contains(successJSON, "journey-secret") {
				t.Fatalf("successful retry code=%d stdout=%s stderr=%s", code, successJSON, stderr)
			}

			for _, format := range []string{"json", "csv"} {
				exportArgs := append([]string{"export", "--format", format, "--json", "15084084"}, journeyFlags...)
				exportArgs = append(exportArgs, "base_date=20260622", "base_time=0500")
				code, exportJSON, stderr := runTest(exportArgs, fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "journey-secret"}, successClient)
				if code != exitOK || stderr != "" || !strings.Contains(exportJSON, `"diagnostic":`) || !strings.Contains(exportJSON, `"time_to_first_success_ms":`) || strings.Contains(exportJSON, "journey-secret") {
					t.Fatalf("%s export code=%d stdout=%s stderr=%s", format, code, exportJSON, stderr)
				}
			}
		})
	}
}

func TestDiagnosticExactMappingCatalog(t *testing.T) {
	type mappingResult struct {
		Determination     string   `json:"determination"`
		AccountableParty  string   `json:"accountable_party"`
		RecommendedAction string   `json:"recommended_action"`
		AvoidActions      []string `json:"avoid_actions"`
	}
	var mapping struct {
		CauseMappings []struct {
			Cause  string        `json:"cause"`
			Result mappingResult `json:"result"`
		} `json:"cause_mappings"`
		Application struct {
			RouteKind           string `json:"route_kind"`
			DirectSubmissionURL bool   `json:"direct_submission_url"`
			Template            string `json:"template"`
		} `json:"operation_application_path_contract"`
	}
	data, err := os.ReadFile(filepath.Join("testdata", "diagnostic-envelope", "mapping.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &mapping); err != nil {
		t.Fatal(err)
	}
	if len(mapping.CauseMappings) != 11 {
		t.Fatalf("cause mappings=%d", len(mapping.CauseMappings))
	}
	for _, contract := range mapping.CauseMappings {
		got := exactDiagnosticResult(contract.Cause, false)
		if got.Determination != contract.Result.Determination || got.AccountableParty != contract.Result.AccountableParty || got.Recommended[0].ActionID != contract.Result.RecommendedAction || !reflect.DeepEqual(actionIDs(got.Avoid), contract.Result.AvoidActions) {
			t.Fatalf("%s differs from mapping.json: got=%#v contract=%#v", contract.Cause, got, contract.Result)
		}
	}
	if mapping.Application.RouteKind != "dataset_application_entry" || mapping.Application.DirectSubmissionURL || mapping.Application.Template != "https://www.data.go.kr/data/{dataset_id}/openapi.do" {
		t.Fatalf("application mapping drift: %#v", mapping.Application)
	}

	files, err := filepath.Glob(filepath.Join("testdata", "diagnostic-envelope", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		var fixture struct {
			Cause     struct{ Code, Determination, Layer string } `json:"cause"`
			Ownership struct {
				AccountableParty string `json:"accountable_party"`
			} `json:"ownership"`
			Actions struct{ Recommended, Avoid []diagnosticAction } `json:"actions"`
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &fixture); err != nil {
			t.Fatal(err)
		}
		got := exactDiagnosticResult(fixture.Cause.Code, fixture.Cause.Code == "provider_outage")
		if got.Determination != fixture.Cause.Determination || got.Layer != fixture.Cause.Layer || got.AccountableParty != fixture.Ownership.AccountableParty || !reflect.DeepEqual(got.Recommended, fixture.Actions.Recommended) || !reflect.DeepEqual(got.Avoid, fixture.Actions.Avoid) {
			t.Fatalf("%s differs from fixture: got=%#v", filepath.Base(path), got)
		}
	}
}

func actionIDs(actions []diagnosticAction) []string {
	ids := make([]string, len(actions))
	for i, action := range actions {
		ids[i] = action.ActionID
	}
	return ids
}

func TestDiagnosticSubjectApplicationAndRedactionBoundary(t *testing.T) {
	plan := diagnosticTestPlan()
	first, ok := diagnosticSubjectForPlan(plan)
	second, ok2 := diagnosticSubjectForPlan(plan)
	if !ok || !ok2 || first != second {
		t.Fatalf("subject is not stable: %#v %#v", first, second)
	}
	entry := datasetApplicationForSubject(first)
	if entry == nil || entry.URL != "https://www.data.go.kr/data/15095335/openapi.do" || entry.DirectSubmissionURL {
		t.Fatalf("application entry = %#v", entry)
	}
	invalid := plan
	invalid.Spec.ID = "dataset/secret?key=leak"
	if _, ok := diagnosticSubjectForPlan(invalid); ok {
		t.Fatal("non-registry dataset accepted")
	}
	handoff := reviewedSuccessfulHandoff(plan, false, true)
	data, err := json.Marshal(handoff)
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(data)
	for _, forbidden := range []string{"serviceKey", "authorization", "response_body", "query_values", "leak"} {
		if strings.Contains(strings.ToLower(serialized), strings.ToLower(forbidden)) {
			t.Fatalf("unsafe field/value %q in %s", forbidden, serialized)
		}
	}
	if handoff.Contract.RuntimeAuthority {
		t.Fatal("draft contract became runtime authority")
	}
	if handoff.Contract.SchemaSHA256 != diagnosticSchemaSHA256 || handoff.Contract.MappingSHA256 != diagnosticMappingSHA256 {
		t.Fatal("contract digest drift")
	}
}

func TestDiagnosticConsumerHandoffDocumentsExactConsumedAndUnsupportedFields(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "diagnostic-consumer-handoff.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(doc)
	for _, required := range []string{
		"## Exact Web field policy", "contract", "subject", "result", "capabilities.dataset_application",
		"capabilities.local_reproduction", "capabilities.reusable_export", "capabilities.public_health",
		"time_to_diagnosis_ms", "time_to_first_success_ms", "ignored by Web", "unsupported handoff inputs",
		"Unknown additive fields", "affected projection", "--journey-started-at", "--journey-diagnosed-at",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("consumer field policy missing %q", required)
		}
	}
}

func TestSyncCommandCarriesExplicitJourneyMetricsAcrossFailureAndSuccess(t *testing.T) {
	startedAt := time.Now().UTC().Add(-time.Second).Format(time.RFC3339Nano)
	failureClient := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusTooManyRequests, Header: make(http.Header), Body: http.NoBody}, nil
	})
	base := []string{"sync", "15084084", "--json", "--journey-started-at", startedAt, "base_date=20260622", "base_time=0500"}
	code, failedJSON, stderr := runTest(append(append([]string{}, base...), "--output-dir", filepath.Join(t.TempDir(), "failed")), fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "sync-secret"}, failureClient)
	if code != exitRequest || stderr != "" {
		t.Fatalf("failed sync code=%d stdout=%s stderr=%s", code, failedJSON, stderr)
	}
	var failed struct {
		Failure struct {
			Diagnostic *localDiagnosticOutcome `json:"diagnostic"`
		} `json:"failure"`
	}
	if err := json.Unmarshal([]byte(failedJSON), &failed); err != nil || failed.Failure.Diagnostic == nil || failed.Failure.Diagnostic.ConsumerHandoff == nil || failed.Failure.Diagnostic.ConsumerHandoff.Metrics == nil {
		t.Fatalf("failed sync metrics missing: err=%v value=%#v", err, failed)
	}
	diagnosedAt := failed.Failure.Diagnostic.Timing.DiagnosisComputedAt
	successClient := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"rows":[{"value":1}]}`))}, nil
	})
	successArgs := []string{"sync", "15084084", "--json", "--journey-started-at", startedAt, "--journey-diagnosed-at", diagnosedAt, "--output-dir", filepath.Join(t.TempDir(), "success"), "base_date=20260622", "base_time=0500"}
	code, successJSON, stderr := runTest(successArgs, fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "sync-secret"}, successClient)
	if code != exitOK || stderr != "" || !strings.Contains(successJSON, `"time_to_diagnosis_ms":`) || !strings.Contains(successJSON, `"time_to_first_success_ms":`) || strings.Contains(successJSON, "sync-secret") {
		t.Fatalf("successful sync code=%d stdout=%s stderr=%s", code, successJSON, stderr)
	}
}

func TestJourneyTimestampFlagsFailClosed(t *testing.T) {
	for _, args := range [][]string{
		{"get", "15084084", "--json", "--journey-started-at", "not-a-time"},
		{"get", "15084084", "--json", "--journey-diagnosed-at", "2026-07-16T00:00:01Z"},
		{"get", "15084084", "--json", "--journey-started-at", "2026-07-16T00:00:02Z", "--journey-diagnosed-at", "2026-07-16T00:00:01Z"},
	} {
		code, stdout, stderr := runTest(args, nil, nil)
		if code != exitUsage || stdout != "" || stderr == "" {
			t.Fatalf("args=%v code=%d stdout=%s stderr=%s", args, code, stdout, stderr)
		}
	}
}

func TestGenericAuthorizationAndResponseOnlyOutageStayConservative(t *testing.T) {
	plan := diagnosticTestPlan()
	for _, status := range []int{401, 403} {
		local := normalizeLocalDiagnosis(localDiagnosticEvidence{Failure: executionFailure{Category: "authentication"}, Envelope: &datago.ResponseEnvelope{StatusCode: status}})
		handoff := reviewedDiagnosticHandoff(localDiagnosticEvidence{Plan: &plan}, local)
		if handoff.Result.Code != "unknown" {
			t.Fatalf("HTTP %d became certain: %#v", status, handoff.Result)
		}
	}
	local := normalizeLocalDiagnosis(localDiagnosticEvidence{Failure: executionFailure{Category: "provider"}, Envelope: &datago.ResponseEnvelope{StatusCode: 503}})
	handoff := reviewedDiagnosticHandoff(localDiagnosticEvidence{Plan: &plan}, local)
	if handoff.Result.Code != "provider_outage" || len(handoff.Result.Avoid) != 0 {
		t.Fatalf("response-only outage overclaimed: %#v", handoff.Result)
	}
}

func TestHumanAndJSONContractDiagnosisAgree(t *testing.T) {
	plan := diagnosticTestPlan()
	failure := attachLocalDiagnosis(localDiagnosticEvidence{
		Failure:  executionFailure{Category: "external_provider", Reason: "provider_temporarily_unavailable"},
		Envelope: &datago.ResponseEnvelope{StatusCode: 503},
		Plan:     &plan,
	})
	var human strings.Builder
	printExecutionFailureBrief(&human, failure)
	if !strings.Contains(human.String(), "diagnosis: provider_outage (accountable=provider, determination=inferred)") || !strings.Contains(human.String(), "action: check_provider_status") || strings.Contains(human.String(), "avoid: reissue_credential") {
		t.Fatalf("human contract output disagrees: %s", human.String())
	}
	data, err := json.Marshal(failure)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"code":"provider_outage"`) || !strings.Contains(string(data), `"action_id":"check_provider_status"`) {
		t.Fatalf("JSON contract output disagrees: %s", data)
	}
}
