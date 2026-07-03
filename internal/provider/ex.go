package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type EXAdapter struct {
	StaticHostMatcher
}

func NewEXAdapter() EXAdapter {
	return EXAdapter{StaticHostMatcher{Hosts: EXHosts()}}
}

func EXHosts() []string {
	return []string{"data.ex.co.kr"}
}

func (a EXAdapter) Name() string { return "ex" }

func (a EXAdapter) Hosts() []string { return EXHosts() }

func (a EXAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a EXAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "ex", a.DependencyClass(req.Spec, req.Operation))
}

func (a EXAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("ex adapter call support is not enabled yet")
}
