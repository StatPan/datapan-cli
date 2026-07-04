package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type HRFCOAdapter struct {
	StaticHostMatcher
}

func NewHRFCOAdapter() HRFCOAdapter {
	return HRFCOAdapter{StaticHostMatcher{Hosts: HRFCOHosts()}}
}

func HRFCOHosts() []string {
	return []string{
		"data.floodmap.go.kr",
		"www.hrfco.go.kr",
	}
}

func (a HRFCOAdapter) Name() string { return "hrfco" }

func (a HRFCOAdapter) Hosts() []string { return HRFCOHosts() }

func (a HRFCOAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a HRFCOAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "hrfco", a.DependencyClass(req.Spec, req.Operation))
}

func (a HRFCOAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("hrfco adapter call support is not enabled yet")
}
