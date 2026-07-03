package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type ChungnamAdapter struct {
	StaticHostMatcher
}

func NewChungnamAdapter() ChungnamAdapter {
	return ChungnamAdapter{StaticHostMatcher{Hosts: ChungnamHosts()}}
}

func ChungnamHosts() []string {
	return []string{
		"alldam.chungnam.go.kr",
		"localfood.chungnam.go.kr",
		"www.chungnam.go.kr",
		"www.xn--6-6v7en42by2es7i6jc.com",
	}
}

func (a ChungnamAdapter) Name() string { return "chungnam" }

func (a ChungnamAdapter) Hosts() []string { return ChungnamHosts() }

func (a ChungnamAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a ChungnamAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "chungnam", a.DependencyClass(req.Spec, req.Operation))
}

func (a ChungnamAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("chungnam adapter call support is not enabled yet")
}
