package cli

import "testing"

func TestParseDataGoKrApplicationFormPreservesHiddenFields(t *testing.T) {
	document := `<html><body><form method="post" action="/iim/api/insertDevAcount.do">
<input type="hidden" name="csrf" value="token">
<input type="hidden" name="publicDataPk" value="15000017">
<textarea name="usePurpsCn"></textarea>
<input type="checkbox" name="agreeYn" value="Y">
<input type="checkbox" data-oprtin-seq-no="123" data-dily-use-expect-co="1000" checked>
<button type="button" onclick="fn_save()">활용신청</button>
</form></body></html>`
	form, err := parseDataGoKrApplicationForm("https://www.data.go.kr/iim/api/selectDevAcountRequestForm.do", document, "공공데이터 활용")
	if err != nil {
		t.Fatal(err)
	}
	if form.action != "https://www.data.go.kr/iim/api/insertDevAcount.do" || form.method != "POST" {
		t.Fatalf("form=%#v", form)
	}
	for key, want := range map[string]string{"csrf": "token", "publicDataPk": "15000017", "usePurpsCn": "공공데이터 활용", "agreeYn": "Y"} {
		if got := form.values.Get(key); got != want {
			t.Fatalf("%s=%q want %q", key, got, want)
		}
	}
	if got := form.values.Get("oprtinAuthorList[0].oprtinSeqNo"); got != "123" {
		t.Fatalf("operation sequence=%q", got)
	}
	if got := form.values.Get("oprtinAuthorList[0].dilyUseExpectCo"); got != "1000" {
		t.Fatalf("daily use=%q", got)
	}
}

func TestClassifyPortalSaveResponse(t *testing.T) {
	for body, want := range map[string]string{
		`{"success":true,"result":true}`:  "access_requested_not_confirmed",
		`{"success":true,"result":false}`: "access_already_requested",
		`{"success":false,"message":"x"}`: "apply_result_rejected",
		`not-json`:                        "apply_result_unconfirmed",
	} {
		if got := classifyPortalSaveResponse(body); got != want {
			t.Fatalf("body=%q got=%q want=%q", body, got, want)
		}
	}
}

func TestParseDataGoKrApplicationFormDiscoversScriptAction(t *testing.T) {
	document := `<form id="requestForm" method="post"><textarea name="purpose"></textarea></form>
<script>function fn_save(){ document.requestForm.action='/iim/api/saveDevAcount.do'; }</script>`
	form, err := parseDataGoKrApplicationForm("https://www.data.go.kr/iim/api/selectDevAcountRequestForm.do", document, "purpose")
	if err != nil {
		t.Fatal(err)
	}
	if form.action != "https://www.data.go.kr/iim/api/saveDevAcount.do" {
		t.Fatalf("action=%q", form.action)
	}
}

func TestParseDataGoKrApplicationFormRejectsExternalAction(t *testing.T) {
	document := `<form method="post" action="https://example.com/collect"><textarea name="purpose"></textarea></form>`
	if _, err := parseDataGoKrApplicationForm("https://www.data.go.kr/form", document, "purpose"); err == nil {
		t.Fatal("external form action was accepted")
	}
}
