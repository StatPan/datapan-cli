package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type KOSISAdapter struct {
	StaticHostMatcher
}

func NewKOSISAdapter() KOSISAdapter {
	return KOSISAdapter{StaticHostMatcher{Hosts: KOSISHosts()}}
}

func KOSISHosts() []string {
	return []string{"kosis.kr"}
}

func (a KOSISAdapter) Name() string { return "kosis" }

func (a KOSISAdapter) Hosts() []string { return KOSISHosts() }

func (a KOSISAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a KOSISAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "kosis", a.DependencyClass(req.Spec, req.Operation))
}

func (a KOSISAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("kosis adapter call support is not enabled yet")
}
