package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type FranchiseFTCAdapter struct {
	StaticHostMatcher
}

func NewFranchiseFTCAdapter() FranchiseFTCAdapter {
	return FranchiseFTCAdapter{StaticHostMatcher{Hosts: FranchiseFTCHosts()}}
}

func FranchiseFTCHosts() []string {
	return []string{"franchise.ftc.go.kr"}
}

func (a FranchiseFTCAdapter) Name() string { return "franchise-ftc" }

func (a FranchiseFTCAdapter) Hosts() []string { return FranchiseFTCHosts() }

func (a FranchiseFTCAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a FranchiseFTCAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "franchise-ftc", a.DependencyClass(req.Spec, req.Operation))
}

func (a FranchiseFTCAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("franchise-ftc adapter call support is not enabled yet")
}
