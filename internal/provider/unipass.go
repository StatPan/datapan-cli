package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type UniPassAdapter struct {
	StaticHostMatcher
}

func NewUniPassAdapter() UniPassAdapter {
	return UniPassAdapter{StaticHostMatcher{Hosts: UniPassHosts()}}
}

func UniPassHosts() []string {
	return []string{"unipass.customs.go.kr"}
}

func (a UniPassAdapter) Name() string { return "unipass" }

func (a UniPassAdapter) Hosts() []string { return UniPassHosts() }

func (a UniPassAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a UniPassAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "unipass", a.DependencyClass(req.Spec, req.Operation))
}

func (a UniPassAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("unipass adapter call support is not enabled yet")
}
