package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type JusoAdapter struct {
	StaticHostMatcher
}

func NewJusoAdapter() JusoAdapter {
	return JusoAdapter{StaticHostMatcher{Hosts: JusoHosts()}}
}

func JusoHosts() []string {
	return []string{"www.juso.go.kr"}
}

func (a JusoAdapter) Name() string { return "juso" }

func (a JusoAdapter) Hosts() []string { return JusoHosts() }

func (a JusoAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a JusoAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "juso", a.DependencyClass(req.Spec, req.Operation))
}

func (a JusoAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("juso adapter call support is not enabled yet")
}
