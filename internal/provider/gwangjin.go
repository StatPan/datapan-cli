package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type GwangjinAdapter struct {
	StaticHostMatcher
}

func NewGwangjinAdapter() GwangjinAdapter {
	return GwangjinAdapter{StaticHostMatcher{Hosts: GwangjinHosts()}}
}

func GwangjinHosts() []string {
	return []string{"www.gwangjin.go.kr"}
}

func (a GwangjinAdapter) Name() string { return "gwangjin" }

func (a GwangjinAdapter) Hosts() []string { return GwangjinHosts() }

func (a GwangjinAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a GwangjinAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "gwangjin", a.DependencyClass(req.Spec, req.Operation))
}

func (a GwangjinAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("gwangjin adapter call support is not enabled yet")
}
