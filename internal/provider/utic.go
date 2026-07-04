package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type UTICAdapter struct {
	StaticHostMatcher
}

func NewUTICAdapter() UTICAdapter {
	return UTICAdapter{StaticHostMatcher{Hosts: UTICHosts()}}
}

func UTICHosts() []string {
	return []string{"www.utic.go.kr"}
}

func (a UTICAdapter) Name() string { return "utic" }

func (a UTICAdapter) Hosts() []string { return UTICHosts() }

func (a UTICAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a UTICAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "utic", a.DependencyClass(req.Spec, req.Operation))
}

func (a UTICAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("utic adapter call support is not enabled yet")
}
