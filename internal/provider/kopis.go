package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type KOPISAdapter struct {
	StaticHostMatcher
}

func NewKOPISAdapter() KOPISAdapter {
	return KOPISAdapter{StaticHostMatcher{Hosts: KOPISHosts()}}
}

func KOPISHosts() []string {
	return []string{"kopis.or.kr"}
}

func (a KOPISAdapter) Name() string { return "kopis" }

func (a KOPISAdapter) Hosts() []string { return KOPISHosts() }

func (a KOPISAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a KOPISAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "kopis", a.DependencyClass(req.Spec, req.Operation))
}

func (a KOPISAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("kopis adapter call support is not enabled yet")
}
