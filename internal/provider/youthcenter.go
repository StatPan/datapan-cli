package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type YouthCenterAdapter struct {
	StaticHostMatcher
}

func NewYouthCenterAdapter() YouthCenterAdapter {
	return YouthCenterAdapter{StaticHostMatcher{Hosts: YouthCenterHosts()}}
}

func YouthCenterHosts() []string {
	return []string{"www.youthcenter.go.kr"}
}

func (a YouthCenterAdapter) Name() string { return "youthcenter" }

func (a YouthCenterAdapter) Hosts() []string { return YouthCenterHosts() }

func (a YouthCenterAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a YouthCenterAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "youthcenter", a.DependencyClass(req.Spec, req.Operation))
}

func (a YouthCenterAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("youthcenter adapter call support is not enabled yet")
}
