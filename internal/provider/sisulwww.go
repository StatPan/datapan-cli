package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type SisulWWWAdapter struct {
	StaticHostMatcher
}

func NewSisulWWWAdapter() SisulWWWAdapter {
	return SisulWWWAdapter{StaticHostMatcher{Hosts: SisulWWWHosts()}}
}

func SisulWWWHosts() []string {
	return []string{"www.sisul.or.kr"}
}

func (a SisulWWWAdapter) Name() string { return "sisul-www" }

func (a SisulWWWAdapter) Hosts() []string { return SisulWWWHosts() }

func (a SisulWWWAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a SisulWWWAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "sisul-www", a.DependencyClass(req.Spec, req.Operation))
}

func (a SisulWWWAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("sisul-www adapter call support is not enabled yet")
}
