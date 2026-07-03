package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type JejuWWWAdapter struct {
	StaticHostMatcher
}

func NewJejuWWWAdapter() JejuWWWAdapter {
	return JejuWWWAdapter{StaticHostMatcher{Hosts: JejuWWWHosts()}}
}

func JejuWWWHosts() []string {
	return []string{"www.jeju.go.kr"}
}

func (a JejuWWWAdapter) Name() string { return "jeju-www" }

func (a JejuWWWAdapter) Hosts() []string { return JejuWWWHosts() }

func (a JejuWWWAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a JejuWWWAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "jeju-www", a.DependencyClass(req.Spec, req.Operation))
}

func (a JejuWWWAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("jeju-www adapter call support is not enabled yet")
}
