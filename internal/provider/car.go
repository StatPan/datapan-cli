package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type CarAdapter struct {
	StaticHostMatcher
}

func NewCarAdapter() CarAdapter {
	return CarAdapter{StaticHostMatcher{Hosts: CarHosts()}}
}

func CarHosts() []string {
	return []string{"car.go.kr"}
}

func (a CarAdapter) Name() string { return "car" }

func (a CarAdapter) Hosts() []string { return CarHosts() }

func (a CarAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a CarAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "car", a.DependencyClass(req.Spec, req.Operation))
}

func (a CarAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("car adapter call support is not enabled yet")
}
