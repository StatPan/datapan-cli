package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type DaeguAdapter struct {
	StaticHostMatcher
}

func NewDaeguAdapter() DaeguAdapter {
	return DaeguAdapter{StaticHostMatcher{Hosts: DaeguHosts()}}
}

func DaeguHosts() []string {
	return []string{
		"air.daegu.go.kr",
		"happy.daegu.go.kr",
		"thegoodnight.daegu.go.kr",
		"www.daegu.go.kr",
		"www.daegufood.go.kr",
	}
}

func (a DaeguAdapter) Name() string { return "daegu" }

func (a DaeguAdapter) Hosts() []string { return DaeguHosts() }

func (a DaeguAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a DaeguAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "daegu", a.DependencyClass(req.Spec, req.Operation))
}

func (a DaeguAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("daegu adapter call support is not enabled yet")
}
