package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type JejuAirAdapter struct {
	StaticHostMatcher
}

func NewJejuAirAdapter() JejuAirAdapter {
	return JejuAirAdapter{StaticHostMatcher{Hosts: JejuAirHosts()}}
}

func JejuAirHosts() []string {
	return []string{"air.jeju.go.kr"}
}

func (a JejuAirAdapter) Name() string { return "jeju-air" }

func (a JejuAirAdapter) Hosts() []string { return JejuAirHosts() }

func (a JejuAirAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a JejuAirAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "jeju-air", a.DependencyClass(req.Spec, req.Operation))
}

func (a JejuAirAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("jeju-air adapter call support is not enabled yet")
}
