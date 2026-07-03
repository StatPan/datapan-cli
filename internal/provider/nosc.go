package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type NOSCAdapter struct {
	StaticHostMatcher
}

func NewNOSCAdapter() NOSCAdapter {
	return NOSCAdapter{StaticHostMatcher{Hosts: NOSCHosts()}}
}

func NOSCHosts() []string {
	return []string{"nosc.go.kr"}
}

func (a NOSCAdapter) Name() string { return "nosc" }

func (a NOSCAdapter) Hosts() []string { return NOSCHosts() }

func (a NOSCAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a NOSCAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "nosc", a.DependencyClass(req.Spec, req.Operation))
}

func (a NOSCAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("nosc adapter call support is not enabled yet")
}
