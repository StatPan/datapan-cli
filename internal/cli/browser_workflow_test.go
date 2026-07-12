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
