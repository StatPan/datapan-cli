package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type CodilAdapter struct {
	StaticHostMatcher
}

func NewCodilAdapter() CodilAdapter {
	return CodilAdapter{StaticHostMatcher{Hosts: CodilHosts()}}
}

func CodilHosts() []string {
	return []string{"www.codil.or.kr"}
}

func (a CodilAdapter) Name() string { return "codil" }

func (a CodilAdapter) Hosts() []string { return CodilHosts() }

func (a CodilAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a CodilAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "codil", a.DependencyClass(req.Spec, req.Operation))
}

func (a CodilAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("codil adapter call support is not enabled yet")
}
