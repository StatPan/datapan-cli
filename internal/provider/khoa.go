package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type KHOAAdapter struct {
	StaticHostMatcher
}

func NewKHOAAdapter() KHOAAdapter {
	return KHOAAdapter{StaticHostMatcher{Hosts: KHOAHosts()}}
}

func KHOAHosts() []string {
	return []string{"www.khoa.go.kr"}
}

func (a KHOAAdapter) Name() string { return "khoa" }

func (a KHOAAdapter) Hosts() []string { return KHOAHosts() }

func (a KHOAAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a KHOAAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "khoa", a.DependencyClass(req.Spec, req.Operation))
}

func (a KHOAAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("khoa adapter call support is not enabled yet")
}
