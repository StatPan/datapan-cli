package cli

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/StatPan/datapan-cli/internal/datago"
)

func TestNormalizeLocalDiagnosisEvidenceBoundaries(t *testing.T) {
	tests := []struct {
		name        string
		evidence    localDiagnosticEvidence
		code        string
		state       string
		responsible string
		prohibited  []string
	}{
		{
			name: "generic forbidden stays unknown",
			evidence: localDiagnosticEvidence{Failure: executionFailure{Category: "approval", Reason: "provider_access_not_approved"},
				Envelope: &datago.ResponseEnvelope{StatusCode: http.StatusForbidden}},
			code: "unknown", state: "unknown", responsible: "unknown",
		},
		{
			name: "generic unauthorized stays unknown",
			evidence: localDiagnosticEvidence{Failure: executionFailure{Category: "authentication", Reason: "provider_rejected_credential"},
				Envelope: &datago.ResponseEnvelope{StatusCode: http.StatusUnauthorized}},
			code: "unknown", state: "unknown", responsible: "unknown",
		},
		{
			name: "bounded registry approval classification is observed",
			evidence: localDiagnosticEvidence{Failure: executionFailure{Category: "approval", Reason: "registry-rule", RegistryRouting: &registryFailureRouting{
				Classification: "approval", RuleID: "approval-rule", MatchKind: "field_equals",
			}}},
			code: "approval_required", state: "observed", responsible: "user",
		},
		{
			name: "registry credential classification remains inferred",
			evidence: localDiagnosticEvidence{Failure: executionFailure{Category: "authentication", Reason: "registry-rule", RegistryRouting: &registryFailureRouting{
				Classification: "credential", RuleID: "credential-rule", MatchKind: "field_equals",
			}}},
			code: "credential_invalid", state: "inferred", responsible: "unknown",
		},
		{
			name: "generic registry http status credential route stays unknown",
			evidence: localDiagnosticEvidence{Failure: executionFailure{Category: "authentication", Reason: "registry-rule", RegistryRouting: &registryFailureRouting{
				Classification: "credential", RuleID: "credential-http-rule", MatchKind: "http_status",
			}}},
			code: "unknown", state: "unknown", responsible: "unknown",
		},
		{
			name: "invalid input",
			evidence: localDiagnosticEvidence{Failure: executionFailure{Category: "input", Reason: "provider_rejected_input"},
				Envelope: &datago.ResponseEnvelope{StatusCode: http.StatusBadRequest}},
			code: "invalid_input", state: "inferred", responsible: "user",
		},
		{
			name: "rate limit",
			evidence: localDiagnosticEvidence{Failure: executionFailure{Category: "external_provider", Reason: "provider_temporarily_unavailable"},
				Envelope: &datago.ResponseEnvelope{StatusCode: http.StatusTooManyRequests}},
			code: "rate_limited", state: "observed", responsible: "provider",
		},
		{
			name: "single provider 5xx is provisional outage evidence",
			evidence: localDiagnosticEvidence{Failure: executionFailure{Category: "external_provider", Reason: "provider_temporarily_unavailable"},
				Envelope: &datago.ResponseEnvelope{StatusCode: http.StatusServiceUnavailable}},
			code: "provider_outage", state: "inferred", responsible: "provider",
		},
		{
			name: "registry upstream outage classification still needs corroboration",
			evidence: localDiagnosticEvidence{Failure: executionFailure{Category: "external_provider", Reason: "registry-rule", RegistryRouting: &registryFailureRouting{
				Classification: "upstream_outage", RuleID: "outage-rule",
			}}},
			code: "provider_outage", state: "inferred", responsible: "provider",
		},
		{
			name: "unexpected response contract",
			evidence: localDiagnosticEvidence{Failure: executionFailure{Category: "adapter", Reason: "unexpected_provider_response_shape"},
				Envelope: &datago.ResponseEnvelope{StatusCode: http.StatusOK, SemanticStatus: "html_response"}},
			code: "contract_drift", state: "inferred", responsible: "datapan",
		},
		{
			name:     "transport failure alone stays unknown",
			evidence: localDiagnosticEvidence{Failure: executionFailure{Category: "external_provider", Reason: "provider_transport_temporarily_failed"}},
			code:     "unknown", state: "unknown", responsible: "unknown",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeLocalDiagnosis(tt.evidence)
			if got.Code != tt.code || got.EvidenceState != tt.state || got.ResponsibleParty != tt.responsible {
				t.Fatalf("diagnosis=%+v", got)
			}
			if len(got.ProhibitedActions) != len(tt.prohibited) || (len(tt.prohibited) > 0 && !reflect.DeepEqual(got.ProhibitedActions, tt.prohibited)) {
				t.Fatalf("prohibited=%v want=%v", got.ProhibitedActions, tt.prohibited)
			}
		})
	}
}

func TestLocalDiagnosticTimingAndCallSucceededScope(t *testing.T) {
	startedAt := time.Date(2026, 7, 16, 3, 4, 5, 123000000, time.FixedZone("KST", 9*60*60))
	endedAt := startedAt.Add(1750 * time.Millisecond)
	computedAt := endedAt.Add(25 * time.Millisecond)
	timing := localDiagnosticAttemptTiming(startedAt, endedAt, computedAt)
	if timing == nil || timing.CallAttemptElapsedMS == nil || *timing.CallAttemptElapsedMS != 1750 {
		t.Fatalf("timing=%+v", timing)
	}
	if timing.CallAttemptStartedAt != "2026-07-15T18:04:05.123Z" || timing.CallAttemptEndedAt != "2026-07-15T18:04:06.873Z" || timing.DiagnosisComputedAt != "2026-07-15T18:04:06.898Z" {
		t.Fatalf("UTC timing=%+v", timing)
	}
	if got := localDiagnosticAttemptTiming(startedAt, endedAt, endedAt.Add(-time.Nanosecond)); got != nil {
		t.Fatalf("pre-completion diagnosis timestamp accepted: %+v", got)
	}
	succeeded := localCallSucceededDiagnosisWithClock(datago.ResponseEnvelope{StatusCode: 200, SemanticStatus: "json_response"}, startedAt, endedAt, func() time.Time { return computedAt })
	if succeeded.Code != "call_succeeded" || succeeded.EvidenceState != "observed" || succeeded.Timing == nil || succeeded.Timing.CallAttemptElapsedMS == nil {
		t.Fatalf("call_succeeded=%+v", succeeded)
	}
	wantScope := []string{"transport", "provider_response", "declared_semantic_status"}
	if !reflect.DeepEqual(succeeded.Scope, wantScope) {
		t.Fatalf("scope=%v want=%v", succeeded.Scope, wantScope)
	}
	failure := attachLocalDiagnosisWithClock(localDiagnosticEvidence{
		Failure:   executionFailure{Category: "external_provider", Reason: "provider_temporarily_unavailable"},
		Envelope:  &datago.ResponseEnvelope{StatusCode: http.StatusServiceUnavailable},
		StartedAt: startedAt,
		EndedAt:   endedAt,
	}, func() time.Time { return computedAt })
	if failure.Diagnostic == nil || !reflect.DeepEqual(failure.Diagnostic.Timing, timing) {
		t.Fatalf("deterministic failure timing=%+v want=%+v", failure.Diagnostic, timing)
	}
}

func TestLocalDiagnosisDoesNotCopyProviderMessagesOrCredentials(t *testing.T) {
	credential := "secret-value"
	encoded := "secret%2Dvalue"
	envelope := datago.ResponseEnvelope{
		StatusCode: http.StatusServiceUnavailable,
		Message:    "reflected " + credential + " " + encoded,
		Body:       `{"authorization":"Bearer secret-value"}`,
		ProviderStatus: &datago.ProviderStatus{
			Message: "provider reflected " + credential,
		},
	}
	got := normalizeLocalDiagnosis(localDiagnosticEvidence{
		Failure:  executionFailure{Category: "external_provider", Reason: "provider_temporarily_unavailable"},
		Envelope: &envelope,
	})
	encodedOutcome := strings.Join(append(append(got.Evidence, got.RecommendedActions...), got.ProhibitedActions...), " ")
	for _, forbidden := range []string{credential, encoded, "Bearer", "authorization"} {
		if strings.Contains(encodedOutcome, forbidden) {
			t.Fatalf("diagnostic copied sensitive provider content %q: %+v", forbidden, got)
		}
	}
	unknown := normalizeLocalDiagnosis(localDiagnosticEvidence{Failure: executionFailure{Reason: "secret-value"}})
	if strings.Contains(strings.Join(unknown.Evidence, " "), credential) {
		t.Fatalf("arbitrary failure reason leaked into evidence: %+v", unknown)
	}
}

func TestGetFailureAddsDiagnosticWithoutChangingExitContract(t *testing.T) {
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: http.NoBody, Header: make(http.Header)}, nil
	})
	code, stdout, stderr := runTest(
		[]string{"get", "15084084", "--json", "base_date=20260622", "base_time=0500"},
		fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "secret-value"}, client,
	)
	if code != exitRequest || stderr != "" {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{
		`"category": "external_provider"`, `"code": "provider_outage"`, `"evidence_state": "inferred"`,
		`"scope": [`, `"provider_call"`, `"call_attempt_started_at":`, `"call_attempt_ended_at":`, `"diagnosis_computed_at":`, `"call_attempt_elapsed_ms":`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	for _, forbidden := range []string{"secret-value", "avoid_key_reissue", "time_to_diagnosis_ms", "time_to_first_success_ms"} {
		if strings.Contains(stdout, forbidden) {
			t.Fatalf("unexpected %q in output: %s", forbidden, stdout)
		}
	}
	assertRuntimeDiagnosticTiming(t, stdout, true)
}

func TestGetSuccessReportsBoundedCallSucceededScopeNotJourneyMetric(t *testing.T) {
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: http.Header{"Content-Type": []string{"application/json"}}}, nil
	})
	code, stdout, stderr := runTest(
		[]string{"get", "15084084", "--json", "base_date=20260622", "base_time=0500"},
		fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "secret-value"}, client,
	)
	if code != exitOK || stderr != "" {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{`"code": "call_succeeded"`, `"transport"`, `"provider_response"`, `"declared_semantic_status"`, `"call_attempt_elapsed_ms":`} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	for _, forbidden := range []string{"secret-value", "time_to_diagnosis_ms", "time_to_first_success_ms", "first_success_at"} {
		if strings.Contains(stdout, forbidden) {
			t.Fatalf("unexpected %q in output: %s", forbidden, stdout)
		}
	}
	assertRuntimeDiagnosticTiming(t, stdout, false)
}

func TestGetGenericAuthenticationStatusesStayUnknownAtRuntime(t *testing.T) {
	for _, tt := range []struct {
		name     string
		status   int
		category string
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, category: "authentication"},
		{name: "forbidden", status: http.StatusForbidden, category: "approval"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tt.status, Body: http.NoBody, Header: make(http.Header)}, nil
			})
			code, stdout, stderr := runTest(
				[]string{"get", "15084084", "--json", "base_date=20260622", "base_time=0500"},
				fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "secret-value"}, client,
			)
			if code != exitRequest || stderr != "" {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
			}
			for _, want := range []string{`"category": "` + tt.category + `"`, `"code": "unknown"`, `"evidence_state": "unknown"`, `"check_local_credential"`, `"check_dataset_approval"`, `"check_provider_status"`} {
				if !strings.Contains(stdout, want) {
					t.Fatalf("expected %q in output: %s", want, stdout)
				}
			}
			for _, forbidden := range []string{"approval_propagating", "avoid_key_reissue", "secret-value"} {
				if strings.Contains(stdout, forbidden) {
					t.Fatalf("unexpected %q in output: %s", forbidden, stdout)
				}
			}
			assertRuntimeDiagnosticTiming(t, stdout, true)
		})
	}
}

func TestSyncFailureUsesReachableRuntimeEvidence(t *testing.T) {
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusTooManyRequests, Body: http.NoBody, Header: make(http.Header)}, nil
	})
	code, stdout, stderr := runTest(
		[]string{"sync", "15084084", "--output-dir", t.TempDir() + "/cache", "--json", "base_date=20260622", "base_time=0500"},
		fakeEnv{"DATAPAN_DATA_GO_KR_KEY": "secret-value"}, client,
	)
	if code != exitRequest || stderr != "" {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	for _, want := range []string{`"category": "external_provider"`, `"code": "rate_limited"`, `"evidence_state": "observed"`, `"http_status:429"`} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in output: %s", want, stdout)
		}
	}
	for _, forbidden := range []string{"approval_propagating", "semantic_quality", "stale_data", "avoid_key_reissue", "secret-value"} {
		if strings.Contains(stdout, forbidden) {
			t.Fatalf("unexpected %q in output: %s", forbidden, stdout)
		}
	}
	assertRuntimeDiagnosticTiming(t, stdout, true)
}

func assertRuntimeDiagnosticTiming(t *testing.T, output string, failure bool) {
	t.Helper()
	var payload struct {
		Diagnostic *localDiagnosticOutcome `json:"diagnostic"`
		Failure    *executionFailure       `json:"failure"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("decode runtime output: %v\n%s", err, output)
	}
	diagnostic := payload.Diagnostic
	if failure && payload.Failure != nil {
		diagnostic = payload.Failure.Diagnostic
	}
	if diagnostic == nil || diagnostic.Timing == nil || diagnostic.Timing.CallAttemptElapsedMS == nil {
		t.Fatalf("runtime diagnostic timing missing: %+v", diagnostic)
	}
	startedAt, err := time.Parse(time.RFC3339Nano, diagnostic.Timing.CallAttemptStartedAt)
	if err != nil {
		t.Fatalf("parse call start: %v", err)
	}
	endedAt, err := time.Parse(time.RFC3339Nano, diagnostic.Timing.CallAttemptEndedAt)
	if err != nil {
		t.Fatalf("parse call end: %v", err)
	}
	computedAt, err := time.Parse(time.RFC3339Nano, diagnostic.Timing.DiagnosisComputedAt)
	if err != nil {
		t.Fatalf("parse diagnosis time: %v", err)
	}
	if endedAt.Before(startedAt) || computedAt.Before(endedAt) || *diagnostic.Timing.CallAttemptElapsedMS != endedAt.Sub(startedAt).Milliseconds() {
		t.Fatalf("invalid runtime timing: %+v", diagnostic.Timing)
	}
}
