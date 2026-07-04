package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type NIHCAdapter struct {
	StaticHostMatcher
}

func NewNIHCAdapter() NIHCAdapter {
	return NIHCAdapter{StaticHostMatcher{Hosts: NIHCHosts()}}
}

func NIHCHosts() []string {
	return []string{"www.nihc.go.kr"}
}

func (a NIHCAdapter) Name() string { return "nihc" }

func (a NIHCAdapter) Hosts() []string { return NIHCHosts() }

func (a NIHCAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a NIHCAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "nihc", a.DependencyClass(req.Spec, req.Operation))
}

func (a NIHCAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("nihc adapter call support is not enabled yet")
}
