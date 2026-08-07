package cli

import (
	"testing"

	"github.com/StatPan/datapan-cli/internal/datago"
)

func TestShouldStopApprovalBatchOnSessionOrHumanGate(t *testing.T) {
	for _, result := range []browserResult{
		{Status: "session_expired_or_login_required"},
		{Status: "manual_login_timeout"},
		{Action: "portal_rate_limited"},
		{Action: "access_user_action_required", HumanGateDetected: true},
	} {
		if !shouldStopApprovalBatch(result) {
			t.Fatalf("batch did not stop for %#v", result)
		}
	}
	if shouldStopApprovalBatch(browserResult{Status: "inspected", Action: "access_requested_not_confirmed"}) {
		t.Fatal("confirmed application result stopped the batch")
	}
}

func TestApprovalResultNeedsReinspectionForFormURL(t *testing.T) {
	result := approvalApplyResult{
		Action:  "access_already_requested",
		Details: map[string]any{"url": "https://www.data.go.kr/iim/api/selectDevAcountRequestForm.do?publicDataDetailPk=x"},
	}
	if !approvalResultNeedsReinspection(result) {
		t.Fatal("form-page inference must be reinspected")
	}
	result.Details["url"] = "https://www.data.go.kr/iim/api/selectAcountList.do?status=dupReq"
	if approvalResultNeedsReinspection(result) {
		t.Fatal("explicit duplicate result must remain terminal")
	}
}

func TestApprovalDependencyProfilesSeparateExternalAuthentication(t *testing.T) {
	reg := datago.NewRegistry([]datago.Spec{
		{ID: "gateway", Provider: "data.go.kr", Operations: []datago.Operation{{Name: "op", Endpoint: "https://apis.data.go.kr/test"}}},
		{ID: "external", Provider: "data.go.kr", Operations: []datago.Operation{{Name: "op", Endpoint: "https://external.example/api"}}},
		{ID: "empty", Provider: "data.go.kr"},
	})
	profiles := approvalDependencyProfiles(reg)
	if !profiles["gateway"].HasGateway || profiles["gateway"].HasExternal {
		t.Fatalf("gateway profile=%#v", profiles["gateway"])
	}
	if !profiles["external"].HasExternal || profiles["external"].HasGateway {
		t.Fatalf("external profile=%#v", profiles["external"])
	}
	if got := profiles["external"].ExternalHosts(); len(got) != 1 || got[0] != "external.example" {
		t.Fatalf("external hosts=%#v", got)
	}
	if got := profiles["empty"].PrimaryClass(); got != "no_operations" {
		t.Fatalf("empty class=%q", got)
	}
}
