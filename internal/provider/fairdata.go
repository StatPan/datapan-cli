package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type FairDataAdapter struct {
	StaticHostMatcher
}

func NewFairDataAdapter() FairDataAdapter {
	return FairDataAdapter{StaticHostMatcher{Hosts: FairDataHosts()}}
}

func FairDataHosts() []string {
	return []string{"www.fairdata.go.kr"}
}

func (a FairDataAdapter) Name() string { return "fairdata" }

func (a FairDataAdapter) Hosts() []string { return FairDataHosts() }

func (a FairDataAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a FairDataAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "fairdata", a.DependencyClass(req.Spec, req.Operation))
}

func (a FairDataAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("fairdata adapter call support is not enabled yet")
}
