package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type CultureAdapter struct {
	StaticHostMatcher
}

func NewCultureAdapter() CultureAdapter {
	return CultureAdapter{StaticHostMatcher{Hosts: CultureHosts()}}
}

func CultureHosts() []string {
	return []string{"www.culture.go.kr"}
}

func (a CultureAdapter) Name() string { return "culture" }

func (a CultureAdapter) Hosts() []string { return CultureHosts() }

func (a CultureAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a CultureAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "culture", a.DependencyClass(req.Spec, req.Operation))
}

func (a CultureAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("culture adapter call support is not enabled yet")
}
