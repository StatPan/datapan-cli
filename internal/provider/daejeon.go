package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type DaejeonAdapter struct {
	StaticHostMatcher
}

func NewDaejeonAdapter() DaejeonAdapter {
	return DaejeonAdapter{StaticHostMatcher{Hosts: DaejeonHosts()}}
}

func DaejeonHosts() []string {
	return []string{
		"bigdata.daejeon.go.kr",
		"gis.daejeon.go.kr",
	}
}

func (a DaejeonAdapter) Name() string { return "daejeon" }

func (a DaejeonAdapter) Hosts() []string { return DaejeonHosts() }

func (a DaejeonAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a DaejeonAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "daejeon", a.DependencyClass(req.Spec, req.Operation))
}

func (a DaejeonAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("daejeon adapter call support is not enabled yet")
}
