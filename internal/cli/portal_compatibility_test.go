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
}
