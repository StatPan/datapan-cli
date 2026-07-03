package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type GIMSAdapter struct {
	StaticHostMatcher
}

func NewGIMSAdapter() GIMSAdapter {
	return GIMSAdapter{StaticHostMatcher{Hosts: GIMSHosts()}}
}

func GIMSHosts() []string {
	return []string{"www.gims.go.kr"}
}

func (a GIMSAdapter) Name() string { return "gims" }

func (a GIMSAdapter) Hosts() []string { return GIMSHosts() }

func (a GIMSAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a GIMSAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "gims", a.DependencyClass(req.Spec, req.Operation))
}

func (a GIMSAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("gims adapter call support is not enabled yet")
}
