package cli

import "testing"

func TestDetectApplicationStateDoesNotTreatApprovalMetadataAsGranted(t *testing.T) {
	state := detectApplicationState("개발계정 자동승인 운영계정 심의승인 활용신청")
	if got := state["status"]; got != "access_user_action_required" {
		t.Fatalf("status=%v state=%#v", got, state)
	}
	if state["has_approved_text"] != false {
		t.Fatalf("generic approval metadata was treated as granted: %#v", state)
	}
}

func TestDetectApplicationStateRecognizesRequestedState(t *testing.T) {
	for _, text := range []string{"활용신청 승인대기", "이미 신청한 API입니다", "신청취소"} {
		state := detectApplicationState(text)
		if got := state["status"]; got != "access_requested_not_confirmed" {
			t.Fatalf("text=%q status=%v state=%#v", text, got, state)
		}
	}
}

func TestLooksRequestedOrGrantedIgnoresGenericApprovalPolicy(t *testing.T) {
	if looksRequestedOrGranted("개발단계 자동승인 운영단계 심의승인") {
		t.Fatal("generic approval policy must not be treated as an application result")
	}
}

func TestClassifyApplyResultRequiresExplicitConfirmation(t *testing.T) {
	if got := classifyApplyResult("활용신청 화면"); got != "apply_result_unconfirmed" {
		t.Fatalf("unconfirmed page classified as %q", got)
	}
	if got := classifyApplyResult("활용신청이 신청되었습니다"); got != "access_requested_not_confirmed" {
		t.Fatalf("confirmed page classified as %q", got)
	}
}

func TestClassifyApplyResultRecognizesDuplicateRequestResultURL(t *testing.T) {
	got := classifyApplyResultAtURL("https://www.data.go.kr/iim/api/selectAcountList.do?status=dupReq", "")
	if got != "access_already_requested" {
		t.Fatalf("duplicate request URL classified as %q", got)
	}
}
