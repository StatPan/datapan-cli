package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type MPVAEgonghunAdapter struct {
	StaticHostMatcher
}

func NewMPVAEgonghunAdapter() MPVAEgonghunAdapter {
	return MPVAEgonghunAdapter{StaticHostMatcher{Hosts: MPVAEgonghunHosts()}}
}

func MPVAEgonghunHosts() []string {
	return []string{"e-gonghun.mpva.go.kr"}
}

func (a MPVAEgonghunAdapter) Name() string { return "mpva-egonghun" }

func (a MPVAEgonghunAdapter) Hosts() []string { return MPVAEgonghunHosts() }

func (a MPVAEgonghunAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a MPVAEgonghunAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "mpva-egonghun", a.DependencyClass(req.Spec, req.Operation))
}

func (a MPVAEgonghunAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("mpva-egonghun adapter call support is not enabled yet")
}
