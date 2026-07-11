package datago

import "testing"

func TestClassifyResponseAcceptsInfo00AsProviderSuccess(t *testing.T) {
	body := []byte(`<response><header><resultCode>INFO-00</resultCode><resultMsg>NORMAL SERVICE</resultMsg></header><body /></response>`)
	ok, semanticStatus, message, providerStatus := ClassifyResponse(200, "application/xml", body)
	if !ok || semanticStatus != "provider_ok" || message != "NORMAL SERVICE" {
		t.Fatalf("unexpected classification: ok=%v semantic=%q message=%q", ok, semanticStatus, message)
	}
	if providerStatus == nil || !providerStatus.OK || providerStatus.Code != "INFO-00" {
		t.Fatalf("unexpected provider status: %#v", providerStatus)
	}
}
