package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type SeogwipoAdapter struct {
	StaticHostMatcher
}

func NewSeogwipoAdapter() SeogwipoAdapter {
	return SeogwipoAdapter{StaticHostMatcher{Hosts: SeogwipoHosts()}}
}

func SeogwipoHosts() []string {
	return []string{"www.seogwipo.go.kr"}
}

func (a SeogwipoAdapter) Name() string { return "seogwipo" }

func (a SeogwipoAdapter) Hosts() []string { return SeogwipoHosts() }

func (a SeogwipoAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a SeogwipoAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "seogwipo", a.DependencyClass(req.Spec, req.Operation))
}

func (a SeogwipoAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("seogwipo adapter call support is not enabled yet")
}
