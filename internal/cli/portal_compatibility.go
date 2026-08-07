package cli

import (
	"net/url"
	"strings"
)

// dataGoKrPortalCompatibilitySchemaVersion is separate from API request
// credentials. A service key is injected only into an API request plan; portal
// inspection neither reads nor represents it.
const dataGoKrPortalCompatibilitySchemaVersion = "datapan.data-go-kr.portal-compatibility.v1"

const (
	portalStateLoggedOut        = "logged_out"
	portalStateEligibleToApply  = "eligible_to_apply"
	portalStateAlreadyRequested = "already_requested_or_approved"
	portalStateUnsupportedForm  = "unsupported_form"
	portalStateHumanGate        = "human_gate"
	portalStateUnknown          = "unknown"
	portalSubmissionBlocked     = "blocked_pending_renewed_form_evidence"
	portalSubmissionManual      = "manual_portal_submission_required"
	portalCompatibilityReadOnly = "read_only"
)

// dataGoKrPortalCompatibility is a bounded, value-free receipt. It must not
// contain cookies, account identity, page contents, form values, or browser
// debugger URLs. Evidence values are classification codes, not copied text.
type dataGoKrPortalCompatibility struct {
	SchemaVersion string   `json:"schema_version"`
	State         string   `json:"state"`
	Mode          string   `json:"mode"`
	Submission    string   `json:"submission"`
	NextAction    string   `json:"next_action"`
	Evidence      []string `json:"evidence"`
	ObservedURL   string   `json:"observed_url,omitempty"`
}

func inspectDataGoKrPortalCompatibility(currentURL, pageText string, controls []map[string]string) dataGoKrPortalCompatibility {
	compatibility := dataGoKrPortalCompatibility{
		SchemaVersion: dataGoKrPortalCompatibilitySchemaVersion,
		Mode:          portalCompatibilityReadOnly,
		Submission:    portalSubmissionBlocked,
		ObservedURL:   redactedDataGoKrPortalURL(currentURL),
	}
	switch {
	case hasHumanGate(pageText):
		compatibility.State, compatibility.NextAction, compatibility.Evidence = portalStateHumanGate, "complete_portal_human_verification", []string{"human_verification_marker"}
	case isDataGoKrLoginPage(currentURL, pageText):
		compatibility.State, compatibility.NextAction, compatibility.Evidence = portalStateLoggedOut, "login_in_local_browser_then_repeat_dry_run", []string{"portal_login_page"}
	case isExplicitDuplicateRequestURL(currentURL):
		compatibility.State, compatibility.NextAction, compatibility.Evidence = portalStateAlreadyRequested, "confirm_per_service_state_in_portal", []string{"duplicate_request_redirect"}
	case hasExplicitRequestedState(pageText):
		compatibility.State, compatibility.NextAction, compatibility.Evidence = portalStateAlreadyRequested, "confirm_per_service_state_in_portal", []string{"explicit_request_status_marker"}
	case hasSupportedApplyControl(controls):
		compatibility.State, compatibility.NextAction, compatibility.Evidence = portalStateEligibleToApply, "review_and_submit_in_portal", []string{"visible_supported_apply_control"}
		compatibility.Submission = portalSubmissionManual
	case strings.Contains(pageText, "활용신청") || strings.Contains(pageText, "개발계정"):
		compatibility.State, compatibility.NextAction, compatibility.Evidence = portalStateUnsupportedForm, "review_renewed_portal_form_and_capture_contract_evidence", []string{"application_markup_without_supported_control"}
	default:
		compatibility.State, compatibility.NextAction, compatibility.Evidence = portalStateUnknown, "open_portal_manually_and_repeat_dry_run", []string{"no_supported_portal_marker"}
	}
	return compatibility
}

func blockedDataGoKrPortalSubmissionCompatibility(applicationURL string) dataGoKrPortalCompatibility {
	return dataGoKrPortalCompatibility{
		SchemaVersion: dataGoKrPortalCompatibilitySchemaVersion,
		State:         portalStateUnsupportedForm,
		Mode:          portalCompatibilityReadOnly,
		Submission:    portalSubmissionBlocked,
		NextAction:    "run_dry_run_or_submit_manually_in_portal",
		Evidence:      []string{"renewed_form_submission_contract_not_evidenced"},
		ObservedURL:   redactedDataGoKrPortalURL(applicationURL),
	}
}

func (c dataGoKrPortalCompatibility) detectedState() map[string]any {
	status := "unknown"
	switch c.State {
	case portalStateEligibleToApply:
		status = "access_user_action_required"
	case portalStateAlreadyRequested:
		// The legacy status deliberately does not distinguish requested from
		// approved. The compatibility state makes that uncertainty explicit.
		status = "access_requested_not_confirmed"
	case portalStateLoggedOut:
		status = "not_logged_in_or_session_expired"
	case portalStateHumanGate:
		status = "human_gate"
	}
	return map[string]any{"status": status, "portal_compatibility": c}
}

func (c dataGoKrPortalCompatibility) inspectionOK() bool {
	return c.State == portalStateEligibleToApply || c.State == portalStateAlreadyRequested
}

func hasSupportedApplyControl(controls []map[string]string) bool {
	for _, control := range controls {
		switch strings.Join(strings.Fields(control["text"]), " ") {
		case "활용신청", "활용 신청", "신청하기":
			return true
		}
	}
	return false
}

func hasExplicitRequestedState(pageText string) bool {
	return strings.Contains(pageText, "이미 신청한 API입니다") || strings.Contains(pageText, "신청취소")
}

func isExplicitDuplicateRequestURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	return err == nil && parsed.Hostname() == "www.data.go.kr" && parsed.Path == "/iim/api/selectAcountList.do" && parsed.Query().Get("status") == "dupReq"
}

func isDataGoKrLoginPage(rawURL, pageText string) bool {
	parsed, err := url.Parse(rawURL)
	if err == nil && (strings.Contains(parsed.Path, "/login/") || parsed.Hostname() == "auth.data.go.kr") {
		return true
	}
	return strings.Contains(pageText, "통합 로그인")
}

func redactedDataGoKrPortalURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || !trustedDataGoKrURL(parsed) {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host + parsed.Path
}
