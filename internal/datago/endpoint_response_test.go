package datago

import "testing"

func TestDaejeonSuccessCodeRequiresExactNormalEnvelopeAndEndpoint(t *testing.T) {
	endpoint := "https://apis.data.go.kr/6300000/openapi2022/restrnt/getrestrnt"
	normal := `{"response":{"header":{"resultCode":"C00","resultMsg":"NORMAL SERVICE"},"body":{"items":[]}}}`
	for _, tc := range []struct {
		name, endpoint, body string
		status               int
		want                 bool
	}{
		{"normal", endpoint, normal, 200, true},
		{"declared-http", "http://apis.data.go.kr/6300000/openapi2022/restrnt/getrestrnt", normal, 200, true},
		{"other-path", "https://apis.data.go.kr/service/other", normal, 200, false},
		{"other-host", "https://example.test/6300000/openapi2022/restrnt/getrestrnt", normal, 200, false},
		{"other-port", "https://apis.data.go.kr:8443/6300000/openapi2022/restrnt/getrestrnt", normal, 200, false},
		{"http-error", endpoint, normal, 500, false},
		{"unknown-code", endpoint, `{"response":{"header":{"resultCode":"C99","resultMsg":"NORMAL SERVICE"},"body":{}}}`, 200, false},
		{"error-message", endpoint, `{"response":{"header":{"resultCode":"C00","resultMsg":"INVALID PARAMETER"},"body":{}}}`, 200, false},
		{"row-only-code", endpoint, `{"rows":[{"resultCode":"C00","resultMsg":"NORMAL SERVICE"}]}`, 200, false},
		{"missing-body", endpoint, `{"response":{"header":{"resultCode":"C00","resultMsg":"NORMAL SERVICE"}}}`, 200, false},
		{"null-body", endpoint, `{"response":{"header":{"resultCode":"C00","resultMsg":"NORMAL SERVICE"},"body":null}}`, 200, false},
		{"scalar-body", endpoint, `{"response":{"header":{"resultCode":"C00","resultMsg":"NORMAL SERVICE"},"body":"error"}}`, 200, false},
		{"array-body", endpoint, `{"response":{"header":{"resultCode":"C00","resultMsg":"NORMAL SERVICE"},"body":["error"]}}`, 200, false},
		{"html", endpoint, `<html>C00 NORMAL SERVICE</html>`, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ok, _, _, status := ClassifyEndpointResponse(tc.endpoint, tc.status, "application/json", []byte(tc.body))
			if ok != tc.want {
				t.Fatalf("success=%v want=%v", ok, tc.want)
			}
			if tc.want && (status == nil || !status.OK || status.Code != "C00") {
				t.Fatal("success must retain provider envelope evidence")
			}
		})
	}
	if ok, _, _, _ := ClassifyResponse(200, "application/json", []byte(normal)); ok {
		t.Fatal("C00 must not become a generic success alias")
	}
}
