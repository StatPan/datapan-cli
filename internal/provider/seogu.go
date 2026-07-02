package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type SeoguAdapter struct {
	StaticHostMatcher
}

func NewSeoguAdapter() SeoguAdapter {
	return SeoguAdapter{StaticHostMatcher{Hosts: SeoguHosts()}}
}

func SeoguHosts() []string {
	return []string{"seogu.go.kr"}
}

func (a SeoguAdapter) Name() string { return "seogu" }

func (a SeoguAdapter) Hosts() []string { return SeoguHosts() }

func (a SeoguAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a SeoguAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "seogu", a.DependencyClass(req.Spec, req.Operation))
}

func (a SeoguAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("seogu adapter call support is not enabled yet")
}
