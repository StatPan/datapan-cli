package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type OpenLawAdapter struct {
	StaticHostMatcher
}

func NewOpenLawAdapter() OpenLawAdapter {
	return OpenLawAdapter{StaticHostMatcher{Hosts: OpenLawHosts()}}
}

func OpenLawHosts() []string {
	return []string{
		"open.law.go.kr",
		"www.law.go.kr",
		"www.lawmaking.go.kr",
	}
}

func (a OpenLawAdapter) Name() string { return "open-law" }

func (a OpenLawAdapter) Hosts() []string { return OpenLawHosts() }

func (a OpenLawAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a OpenLawAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "open-law", a.DependencyClass(req.Spec, req.Operation))
}

func (a OpenLawAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("open-law adapter call support is not enabled yet")
}
