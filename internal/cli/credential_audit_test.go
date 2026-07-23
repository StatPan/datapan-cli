package cli

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/StatPan/datapan-cli/internal/datago"
	providers "github.com/StatPan/datapan-cli/internal/provider"
)

func TestCredentialForOperationNeverFallsBackAcrossProviderGroups(t *testing.T) {
	opendart := datago.Operation{Name: "list", Endpoint: "https://opendart.fss.or.kr/api/list.json"}
	seoul := datago.Operation{Name: "list", Endpoint: "http://openapi.seoul.go.kr:8088/sample/json/service/1/1/"}

	app := app{env: fakeEnv{"DATA_PORTAL_API_KEY": "data-go-kr-secret"}}
	for _, op := range []datago.Operation{opendart, seoul} {
		group, credential, present, err := app.credentialForOperation(op)
		if err != nil || present || credential.Value != "" {
			t.Fatalf("%s used a data.go.kr credential: group=%#v credential=%#v present=%v err=%v", op.Endpoint, group, credential, present, err)
		}
	}

	app.env = fakeEnv{"DATA_PORTAL_API_KEY": "data-go-kr-secret", "OPENDART_API_KEY": "dart-secret", "SEOUL_OPEN_DATA_KEY": "seoul-secret"}
	for _, tc := range []struct {
		op         datago.Operation
		group, env string
	}{
		{opendart, "opendart", "OPENDART_API_KEY"},
		{seoul, "seoul_open_data", "SEOUL_OPEN_DATA_KEY"},
	} {
		group, credential, present, err := app.credentialForOperation(tc.op)
		if err != nil || !present || group.ID != tc.group || credential.Name != tc.env {
			t.Fatalf("unexpected credential resolution: group=%#v credential=%#v present=%v err=%v", group, credential, present, err)
		}
	}
}

func TestCredentialAuditClassifiesBoundedFakeHTTPOutcomes(t *testing.T) {
	candidate := datago.VerificationCandidate{
		Spec:         datago.Spec{ID: "dart", Title: "DART", Provider: "OpenDART"},
		Operation:    datago.Operation{Name: "list", Endpoint: "https://opendart.fss.or.kr/api/list.json"},
		EndpointHost: "opendart.fss.or.kr",
	}
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		missing  []string
		key      string
		want     string
		requests int
	}{
		{"verified", 200, `{"resultCode":"00"}`, nil, "audit-secret", "verified", 1},
		{"approval", 200, `{"resultCode":"30","resultMsg":"not registered for this API"}`, nil, "audit-secret", "approval_required", 1},
		{"approval redacts echoed credential", 200, `{"resultCode":"30","resultMsg":"audit-secret not registered"}`, nil, "audit-secret", "approval_required", 1},
		{"rate", 429, `slow down`, nil, "audit-secret", "rate_limited", 1},
		{"provider", 503, `unavailable`, nil, "audit-secret", "provider_unavailable", 1},
		{"credential", 403, `forbidden`, nil, "audit-secret", "credential_invalid", 1},
		{"input", 0, "", []string{"corp_code"}, "audit-secret", "input_required", 0},
		{"missing auth", 0, "", nil, "", "missing_auth", 0},
		{"unknown", 400, `bad request`, nil, "audit-secret", "unknown", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			a := app{http: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				requests++
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			current := candidate
			current.MissingParams = tc.missing
			result := a.auditVerification(current, providers.Credential{Name: "OPENDART_API_KEY", Value: tc.key}, time.Second)
			_, got, _ := classifyCredentialAudit(result)
			if got != tc.want || requests != tc.requests {
				t.Fatalf("category=%s requests=%d result=%#v", got, requests, result)
			}
			if strings.Contains(result.Reason, "audit-secret") || strings.Contains(result.URL, "audit-secret") {
				t.Fatalf("credential leaked from audit result: %#v", result)
			}
		})
	}
}
