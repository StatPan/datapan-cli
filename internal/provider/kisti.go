package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type KISTIAdapter struct {
	StaticHostMatcher
}

func NewKISTIAdapter() KISTIAdapter {
	return KISTIAdapter{StaticHostMatcher{Hosts: KISTIHosts()}}
}

func KISTIHosts() []string {
	return []string{
		"aida.kisti.re.kr",
		"scienceon.kisti.re.kr",
		"www.ntis.go.kr",
	}
}

func (a KISTIAdapter) Name() string { return "kisti" }

func (a KISTIAdapter) Hosts() []string { return KISTIHosts() }

func (a KISTIAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a KISTIAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "kisti", a.DependencyClass(req.Spec, req.Operation))
}

func (a KISTIAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("kisti adapter call support is not enabled yet")
}
