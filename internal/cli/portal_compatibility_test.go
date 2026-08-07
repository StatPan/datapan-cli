package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type portalCompatibilityFixture struct {
	Name          string              `json:"name"`
	URL           string              `json:"url"`
	PageText      string              `json:"page_text"`
	Controls      []map[string]string `json:"controls"`
	ExpectedState string              `json:"expected_state"`
}

func TestDataGoKrPortalCompatibilityFixtures(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "data-go-kr-portal-compatibility.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []portalCompatibilityFixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			got := inspectDataGoKrPortalCompatibility(fixture.URL, fixture.PageText, fixture.Controls)
			if got.SchemaVersion != dataGoKrPortalCompatibilitySchemaVersion || got.State != fixture.ExpectedState {
				t.Fatalf("compatibility=%#v", got)
			}
			if got.Mode != portalCompatibilityReadOnly || got.Submission == "" || got.NextAction == "" || len(got.Evidence) == 0 {
				t.Fatalf("incomplete compatibility receipt=%#v", got)
			}
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "redacted") || strings.Contains(got.ObservedURL, "?") {
				t.Fatalf("query value leaked into receipt: %s", encoded)
			}
		})
	}
}

func TestDataGoKrPortalCompatibilityDoesNotInferApprovalFromGenericText(t *testing.T) {
	got := inspectDataGoKrPortalCompatibility(
		"https://www.data.go.kr/data/15126469/openapi.do",
		"개발계정 자동승인 운영계정 심의승인 활용신청",
		nil,
	)
	if got.State != portalStateUnsupportedForm {
		t.Fatalf("generic approval policy was inferred as state: %#v", got)
	}
}

func TestDataGoKrPortalCompatibilityDoesNotTreatButtonTextAsEligible(t *testing.T) {
	got := inspectDataGoKrPortalCompatibility(
		"https://www.data.go.kr/data/15126469/openapi.do",
		"API 상세",
		[]map[string]string{{"text": "활용신청", "target_url": "https://www.data.go.kr/data/15126469/openapi.do", "dataset_id": "15126469"}},
	)
	if got.State != portalStateUnsupportedForm || got.Evidence[0] != "apply_control_without_evidenced_service_contract" {
		t.Fatalf("button text was treated as deterministic eligibility: %#v", got)
	}
}

func TestReadOnlyControlReceiptRemovesHrefAndOnclickFromStdoutAndFile(t *testing.T) {
	controls := []map[string]string{{
		"text":    "활용신청",
		"href":    "https://www.data.go.kr/data/15126469/openapi.do?sentinel-query=must-not-leak",
		"onclick": "submitApplication('sentinel-onclick-must-not-leak')",
	}}
	output := filepath.Join(t.TempDir(), "receipt.json")
	var stdout bytes.Buffer
	code := writeWorkflowResultForOptions(&stdout, browserResult{
		OK: true, Command: "submit", Provider: "data.go.kr", Status: "inspected", DryRun: true,
		DetectedState: map[string]any{"apply_controls": controls},
	}, browserWorkflowOptions{Output: output})
	if code != exitOK {
		t.Fatalf("code=%d output=%s", code, stdout.String())
	}
	file, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	for _, receipt := range []string{stdout.String(), string(file)} {
		for _, forbidden := range []string{"sentinel-query", "sentinel-onclick", "href", "onclick"} {
			if strings.Contains(receipt, forbidden) {
				t.Fatalf("read-only receipt leaked %q: %s", forbidden, receipt)
			}
		}
		if !strings.Contains(receipt, "possible_apply_control") || !strings.Contains(receipt, "활용신청") {
			t.Fatalf("safe control summary missing: %s", receipt)
		}
	}
}

func TestPortalSubmissionIsBlockedBeforeBrowserOrHTTPActivity(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "profile")
	var stdout bytes.Buffer
	code := runBrowserWorkflow(browserWorkflowOptions{
		Command: "submit", Apply: true, ListID: "15126469",
		ApplicationURL: "https://www.data.go.kr/data/15126469/openapi.do?session=must-not-leak",
		ProfileDir:     profile, BrowserPath: "/does/not/exist",
	}, &stdout, &bytes.Buffer{})
	if code != exitRequest || strings.Contains(stdout.String(), "must-not-leak") {
		t.Fatalf("code=%d output=%s", code, stdout.String())
	}
	if _, err := os.Stat(profile); !os.IsNotExist(err) {
		t.Fatalf("submission started a browser profile: %v", err)
	}
	var result browserResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != "portal_submission_blocked_pending_form_contract" || result.PortalCompatibility == nil || result.PortalCompatibility.Submission != portalSubmissionBlocked {
		t.Fatalf("result=%#v", result)
	}

	resultMap := (&dataGoKrHTTPSession{}).apply("15126469", "ignored")
	if resultMap["action"] != "portal_submission_blocked_pending_form_contract" {
		t.Fatalf("HTTP submission was not blocked: %#v", resultMap)
	}
	if compatibility, ok := resultMap["portal_compatibility"].(dataGoKrPortalCompatibility); !ok || compatibility.Submission != portalSubmissionBlocked {
		t.Fatalf("HTTP receipt was not preserved: %#v", resultMap)
	}
}
