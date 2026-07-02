package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type GwangmyeongAdapter struct {
	StaticHostMatcher
}

func NewGwangmyeongAdapter() GwangmyeongAdapter {
	return GwangmyeongAdapter{StaticHostMatcher{Hosts: GwangmyeongHosts()}}
}

func GwangmyeongHosts() []string {
	return []string{"data.gm.go.kr"}
}

func (a GwangmyeongAdapter) Name() string { return "gwangmyeong" }

func (a GwangmyeongAdapter) Hosts() []string { return GwangmyeongHosts() }

func (a GwangmyeongAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a GwangmyeongAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "gwangmyeong", a.DependencyClass(req.Spec, req.Operation))
}

func (a GwangmyeongAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("gwangmyeong adapter call support is not enabled yet")
}
