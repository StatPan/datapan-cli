package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type NCPMSAdapter struct {
	StaticHostMatcher
}

func NewNCPMSAdapter() NCPMSAdapter {
	return NCPMSAdapter{StaticHostMatcher{Hosts: NCPMSHosts()}}
}

func NCPMSHosts() []string {
	return []string{"ncpms.rda.go.kr"}
}

func (a NCPMSAdapter) Name() string { return "ncpms" }

func (a NCPMSAdapter) Hosts() []string { return NCPMSHosts() }

func (a NCPMSAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a NCPMSAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "ncpms", a.DependencyClass(req.Spec, req.Operation))
}

func (a NCPMSAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("ncpms adapter call support is not enabled yet")
}
