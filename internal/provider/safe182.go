package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type Safe182Adapter struct {
	StaticHostMatcher
}

func NewSafe182Adapter() Safe182Adapter {
	return Safe182Adapter{StaticHostMatcher{Hosts: Safe182Hosts()}}
}

func Safe182Hosts() []string {
	return []string{"www.safe182.go.kr"}
}

func (a Safe182Adapter) Name() string { return "safe182" }

func (a Safe182Adapter) Hosts() []string { return Safe182Hosts() }

func (a Safe182Adapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a Safe182Adapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "safe182", a.DependencyClass(req.Spec, req.Operation))
}

func (a Safe182Adapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("safe182 adapter call support is not enabled yet")
}
