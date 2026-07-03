package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type MAFRALegacyAdapter struct {
	StaticHostMatcher
}

func NewMAFRALegacyAdapter() MAFRALegacyAdapter {
	return MAFRALegacyAdapter{StaticHostMatcher{Hosts: MAFRALegacyHosts()}}
}

func MAFRALegacyHosts() []string {
	return []string{"211.237.50.150", "211.237.50.150:7080"}
}

func (a MAFRALegacyAdapter) Name() string { return "mafra-legacy" }

func (a MAFRALegacyAdapter) Hosts() []string { return MAFRALegacyHosts() }

func (a MAFRALegacyAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a MAFRALegacyAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "mafra-legacy", a.DependencyClass(req.Spec, req.Operation))
}

func (a MAFRALegacyAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("mafra-legacy adapter call support is not enabled yet")
}
