package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type NRFAdapter struct {
	StaticHostMatcher
}

func NewNRFAdapter() NRFAdapter {
	return NRFAdapter{StaticHostMatcher{Hosts: NRFHosts()}}
}

func NRFHosts() []string {
	return []string{
		"www.kci.go.kr",
		"www.krm.or.kr",
	}
}

func (a NRFAdapter) Name() string { return "nrf" }

func (a NRFAdapter) Hosts() []string { return NRFHosts() }

func (a NRFAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a NRFAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "nrf", a.DependencyClass(req.Spec, req.Operation))
}

func (a NRFAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("nrf adapter call support is not enabled yet")
}
