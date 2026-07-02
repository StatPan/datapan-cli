package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type HappySDAdapter struct {
	StaticHostMatcher
}

func NewHappySDAdapter() HappySDAdapter {
	return HappySDAdapter{StaticHostMatcher{Hosts: HappySDHosts()}}
}

func HappySDHosts() []string {
	return []string{"www.happysd.or.kr"}
}

func (a HappySDAdapter) Name() string { return "happysd" }

func (a HappySDAdapter) Hosts() []string { return HappySDHosts() }

func (a HappySDAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a HappySDAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "happysd", a.DependencyClass(req.Spec, req.Operation))
}

func (a HappySDAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("happysd adapter call support is not enabled yet")
}
