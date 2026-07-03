package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type GICOMSAdapter struct {
	StaticHostMatcher
}

func NewGICOMSAdapter() GICOMSAdapter {
	return GICOMSAdapter{StaticHostMatcher{Hosts: GICOMSHosts()}}
}

func GICOMSHosts() []string {
	return []string{"www.gicoms.go.kr"}
}

func (a GICOMSAdapter) Name() string { return "gicoms" }

func (a GICOMSAdapter) Hosts() []string { return GICOMSHosts() }

func (a GICOMSAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a GICOMSAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "gicoms", a.DependencyClass(req.Spec, req.Operation))
}

func (a GICOMSAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("gicoms adapter call support is not enabled yet")
}
