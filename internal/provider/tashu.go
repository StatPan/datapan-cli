package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type TashuAdapter struct {
	StaticHostMatcher
}

func NewTashuAdapter() TashuAdapter {
	return TashuAdapter{StaticHostMatcher{Hosts: TashuHosts()}}
}

func TashuHosts() []string {
	return []string{"bike.tashu.or.kr"}
}

func (a TashuAdapter) Name() string { return "tashu" }

func (a TashuAdapter) Hosts() []string { return TashuHosts() }

func (a TashuAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a TashuAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "tashu", a.DependencyClass(req.Spec, req.Operation))
}

func (a TashuAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("tashu adapter call support is not enabled yet")
}
