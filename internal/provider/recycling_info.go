package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type RecyclingInfoAdapter struct {
	StaticHostMatcher
}

func NewRecyclingInfoAdapter() RecyclingInfoAdapter {
	return RecyclingInfoAdapter{StaticHostMatcher{Hosts: RecyclingInfoHosts()}}
}

func RecyclingInfoHosts() []string {
	return []string{"www.recycling-info.or.kr"}
}

func (a RecyclingInfoAdapter) Name() string { return "recycling-info" }

func (a RecyclingInfoAdapter) Hosts() []string { return RecyclingInfoHosts() }

func (a RecyclingInfoAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a RecyclingInfoAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "recycling-info", a.DependencyClass(req.Spec, req.Operation))
}

func (a RecyclingInfoAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("recycling-info adapter call support is not enabled yet")
}
