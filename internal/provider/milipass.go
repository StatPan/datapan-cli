package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type MilipassAdapter struct {
	StaticHostMatcher
}

func NewMilipassAdapter() MilipassAdapter {
	return MilipassAdapter{StaticHostMatcher{Hosts: MilipassHosts()}}
}

func MilipassHosts() []string {
	return []string{"www.milipass.kr"}
}

func (a MilipassAdapter) Name() string { return "milipass" }

func (a MilipassAdapter) Hosts() []string { return MilipassHosts() }

func (a MilipassAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a MilipassAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "milipass", a.DependencyClass(req.Spec, req.Operation))
}

func (a MilipassAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("milipass adapter call support is not enabled yet")
}
