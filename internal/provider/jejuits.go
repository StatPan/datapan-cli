package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type JejuITSAdapter struct {
	StaticHostMatcher
}

func NewJejuITSAdapter() JejuITSAdapter {
	return JejuITSAdapter{StaticHostMatcher{Hosts: JejuITSHosts()}}
}

func JejuITSHosts() []string {
	return []string{"www.jejuits.go.kr"}
}

func (a JejuITSAdapter) Name() string { return "jejuits" }

func (a JejuITSAdapter) Hosts() []string { return JejuITSHosts() }

func (a JejuITSAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a JejuITSAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "jejuits", a.DependencyClass(req.Spec, req.Operation))
}

func (a JejuITSAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("jejuits adapter call support is not enabled yet")
}
