package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type DGFCAAdapter struct {
	StaticHostMatcher
}

func NewDGFCAAdapter() DGFCAAdapter {
	return DGFCAAdapter{StaticHostMatcher{Hosts: DGFCAHosts()}}
}

func DGFCAHosts() []string {
	return []string{"dgfca.or.kr"}
}

func (a DGFCAAdapter) Name() string { return "dgfca" }

func (a DGFCAAdapter) Hosts() []string { return DGFCAHosts() }

func (a DGFCAAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a DGFCAAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "dgfca", a.DependencyClass(req.Spec, req.Operation))
}

func (a DGFCAAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("dgfca adapter call support is not enabled yet")
}
