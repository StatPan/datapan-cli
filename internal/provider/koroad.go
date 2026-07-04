package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type KOROADAdapter struct {
	StaticHostMatcher
}

func NewKOROADAdapter() KOROADAdapter {
	return KOROADAdapter{StaticHostMatcher{Hosts: KOROADHosts()}}
}

func KOROADHosts() []string {
	return []string{"opendata.koroad.or.kr"}
}

func (a KOROADAdapter) Name() string { return "koroad" }

func (a KOROADAdapter) Hosts() []string { return KOROADHosts() }

func (a KOROADAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a KOROADAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "koroad", a.DependencyClass(req.Spec, req.Operation))
}

func (a KOROADAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("koroad adapter call support is not enabled yet")
}
