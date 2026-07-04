package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type YuseongAdapter struct {
	StaticHostMatcher
}

func NewYuseongAdapter() YuseongAdapter {
	return YuseongAdapter{StaticHostMatcher{Hosts: YuseongHosts()}}
}

func YuseongHosts() []string {
	return []string{"www.yuseong.go.kr"}
}

func (a YuseongAdapter) Name() string { return "yuseong" }

func (a YuseongAdapter) Hosts() []string { return YuseongHosts() }

func (a YuseongAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a YuseongAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "yuseong", a.DependencyClass(req.Spec, req.Operation))
}

func (a YuseongAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("yuseong adapter call support is not enabled yet")
}
