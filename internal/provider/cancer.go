package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type CancerAdapter struct {
	StaticHostMatcher
}

func NewCancerAdapter() CancerAdapter {
	return CancerAdapter{StaticHostMatcher{Hosts: CancerHosts()}}
}

func CancerHosts() []string {
	return []string{"cancer.go.kr"}
}

func (a CancerAdapter) Name() string { return "cancer" }

func (a CancerAdapter) Hosts() []string { return CancerHosts() }

func (a CancerAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a CancerAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "cancer", a.DependencyClass(req.Spec, req.Operation))
}

func (a CancerAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("cancer adapter call support is not enabled yet")
}
