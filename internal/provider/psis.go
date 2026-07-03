package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type PSISAdapter struct {
	StaticHostMatcher
}

func NewPSISAdapter() PSISAdapter {
	return PSISAdapter{StaticHostMatcher{Hosts: PSISHosts()}}
}

func PSISHosts() []string {
	return []string{"psis.rda.go.kr"}
}

func (a PSISAdapter) Name() string { return "psis" }

func (a PSISAdapter) Hosts() []string { return PSISHosts() }

func (a PSISAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a PSISAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "psis", a.DependencyClass(req.Spec, req.Operation))
}

func (a PSISAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("psis adapter call support is not enabled yet")
}
