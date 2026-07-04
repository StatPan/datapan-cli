package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type AnyangAdapter struct {
	StaticHostMatcher
}

func NewAnyangAdapter() AnyangAdapter {
	return AnyangAdapter{StaticHostMatcher{Hosts: AnyangHosts()}}
}

func AnyangHosts() []string {
	return []string{"www.anyang.go.kr"}
}

func (a AnyangAdapter) Name() string { return "anyang" }

func (a AnyangAdapter) Hosts() []string { return AnyangHosts() }

func (a AnyangAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a AnyangAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "anyang", a.DependencyClass(req.Spec, req.Operation))
}

func (a AnyangAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("anyang adapter call support is not enabled yet")
}
