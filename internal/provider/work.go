package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type WorkAdapter struct {
	StaticHostMatcher
}

func NewWorkAdapter() WorkAdapter {
	return WorkAdapter{StaticHostMatcher{Hosts: WorkHosts()}}
}

func WorkHosts() []string {
	return []string{"openapi.work.go.kr"}
}

func (a WorkAdapter) Name() string { return "work" }

func (a WorkAdapter) Hosts() []string { return WorkHosts() }

func (a WorkAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a WorkAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "work", a.DependencyClass(req.Spec, req.Operation))
}

func (a WorkAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("work adapter call support is not enabled yet")
}
