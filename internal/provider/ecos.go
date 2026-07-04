package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type ECOSAdapter struct {
	StaticHostMatcher
}

func NewECOSAdapter() ECOSAdapter {
	return ECOSAdapter{StaticHostMatcher{Hosts: ECOSHosts()}}
}

func ECOSHosts() []string {
	return []string{"ecos.bok.or.kr"}
}

func (a ECOSAdapter) Name() string { return "ecos" }

func (a ECOSAdapter) Hosts() []string { return ECOSHosts() }

func (a ECOSAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a ECOSAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "ecos", a.DependencyClass(req.Spec, req.Operation))
}

func (a ECOSAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("ecos adapter call support is not enabled yet")
}
