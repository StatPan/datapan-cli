package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type KOAGIAdapter struct {
	StaticHostMatcher
}

func NewKOAGIAdapter() KOAGIAdapter {
	return KOAGIAdapter{StaticHostMatcher{Hosts: KOAGIHosts()}}
}

func KOAGIHosts() []string {
	return []string{"seedpedia.koagi.or.kr"}
}

func (a KOAGIAdapter) Name() string { return "koagi" }

func (a KOAGIAdapter) Hosts() []string { return KOAGIHosts() }

func (a KOAGIAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a KOAGIAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "koagi", a.DependencyClass(req.Spec, req.Operation))
}

func (a KOAGIAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("koagi adapter call support is not enabled yet")
}
