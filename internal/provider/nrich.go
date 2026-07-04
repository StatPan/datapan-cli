package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type NRichAdapter struct {
	StaticHostMatcher
}

func NewNRichAdapter() NRichAdapter {
	return NRichAdapter{StaticHostMatcher{Hosts: NRichHosts()}}
}

func NRichHosts() []string {
	return []string{"portal.nrich.go.kr", "www.nrich.go.kr"}
}

func (a NRichAdapter) Name() string { return "nrich" }

func (a NRichAdapter) Hosts() []string { return NRichHosts() }

func (a NRichAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a NRichAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "nrich", a.DependencyClass(req.Spec, req.Operation))
}

func (a NRichAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("nrich adapter call support is not enabled yet")
}
