package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type CalspiaAdapter struct {
	StaticHostMatcher
}

func NewCalspiaAdapter() CalspiaAdapter {
	return CalspiaAdapter{StaticHostMatcher{Hosts: CalspiaHosts()}}
}

func CalspiaHosts() []string {
	return []string{"www.calspia.go.kr"}
}

func (a CalspiaAdapter) Name() string { return "calspia" }

func (a CalspiaAdapter) Hosts() []string { return CalspiaHosts() }

func (a CalspiaAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a CalspiaAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "calspia", a.DependencyClass(req.Spec, req.Operation))
}

func (a CalspiaAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("calspia adapter call support is not enabled yet")
}
