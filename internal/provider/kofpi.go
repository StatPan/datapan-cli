package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type KOFPIAdapter struct {
	StaticHostMatcher
}

func NewKOFPIAdapter() KOFPIAdapter {
	return KOFPIAdapter{StaticHostMatcher{Hosts: KOFPIHosts()}}
}

func KOFPIHosts() []string {
	return []string{"www.kofpi.or.kr"}
}

func (a KOFPIAdapter) Name() string { return "kofpi" }

func (a KOFPIAdapter) Hosts() []string { return KOFPIHosts() }

func (a KOFPIAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a KOFPIAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "kofpi", a.DependencyClass(req.Spec, req.Operation))
}

func (a KOFPIAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("kofpi adapter call support is not enabled yet")
}
