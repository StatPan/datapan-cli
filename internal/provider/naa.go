package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type NAAAdapter struct {
	StaticHostMatcher
}

func NewNAAAdapter() NAAAdapter {
	return NAAAdapter{StaticHostMatcher{Hosts: NAAHosts()}}
}

func NAAHosts() []string {
	return []string{"www.naa.go.kr"}
}

func (a NAAAdapter) Name() string { return "naa" }

func (a NAAAdapter) Hosts() []string { return NAAHosts() }

func (a NAAAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a NAAAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "naa", a.DependencyClass(req.Spec, req.Operation))
}

func (a NAAAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("naa adapter call support is not enabled yet")
}
