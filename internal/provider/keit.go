package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type KEITAdapter struct {
	StaticHostMatcher
}

func NewKEITAdapter() KEITAdapter {
	return KEITAdapter{StaticHostMatcher{Hosts: KEITHosts()}}
}

func KEITHosts() []string {
	return []string{
		"www.nabis.go.kr",
		"www.sobujang.net",
	}
}

func (a KEITAdapter) Name() string { return "keit" }

func (a KEITAdapter) Hosts() []string { return KEITHosts() }

func (a KEITAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a KEITAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "keit", a.DependencyClass(req.Spec, req.Operation))
}

func (a KEITAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("keit adapter call support is not enabled yet")
}
