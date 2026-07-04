package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type UlsanWWWAdapter struct {
	StaticHostMatcher
}

func NewUlsanWWWAdapter() UlsanWWWAdapter {
	return UlsanWWWAdapter{StaticHostMatcher{Hosts: UlsanWWWHosts()}}
}

func UlsanWWWHosts() []string {
	return []string{"www.ulsan.go.kr"}
}

func (a UlsanWWWAdapter) Name() string { return "ulsan-www" }

func (a UlsanWWWAdapter) Hosts() []string { return UlsanWWWHosts() }

func (a UlsanWWWAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a UlsanWWWAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "ulsan-www", a.DependencyClass(req.Spec, req.Operation))
}

func (a UlsanWWWAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("ulsan-www adapter call support is not enabled yet")
}
