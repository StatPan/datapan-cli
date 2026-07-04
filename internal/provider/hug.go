package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type HUGAdapter struct {
	StaticHostMatcher
}

func NewHUGAdapter() HUGAdapter {
	return HUGAdapter{StaticHostMatcher{Hosts: HUGHosts()}}
}

func HUGHosts() []string {
	return []string{"www.khug.or.kr"}
}

func (a HUGAdapter) Name() string { return "hug" }

func (a HUGAdapter) Hosts() []string { return HUGHosts() }

func (a HUGAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a HUGAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "hug", a.DependencyClass(req.Spec, req.Operation))
}

func (a HUGAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("hug adapter call support is not enabled yet")
}
