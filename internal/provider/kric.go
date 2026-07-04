package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type KRICAdapter struct {
	StaticHostMatcher
}

func NewKRICAdapter() KRICAdapter {
	return KRICAdapter{StaticHostMatcher{Hosts: KRICHosts()}}
}

func KRICHosts() []string {
	return []string{"data.kric.go.kr"}
}

func (a KRICAdapter) Name() string { return "kric" }

func (a KRICAdapter) Hosts() []string { return KRICHosts() }

func (a KRICAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a KRICAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "kric", a.DependencyClass(req.Spec, req.Operation))
}

func (a KRICAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("kric adapter call support is not enabled yet")
}
