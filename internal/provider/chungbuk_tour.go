package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type ChungbukTourAdapter struct {
	StaticHostMatcher
}

func NewChungbukTourAdapter() ChungbukTourAdapter {
	return ChungbukTourAdapter{StaticHostMatcher{Hosts: ChungbukTourHosts()}}
}

func ChungbukTourHosts() []string {
	return []string{"tour.chungbuk.go.kr"}
}

func (a ChungbukTourAdapter) Name() string { return "chungbuk-tour" }

func (a ChungbukTourAdapter) Hosts() []string { return ChungbukTourHosts() }

func (a ChungbukTourAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a ChungbukTourAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "chungbuk-tour", a.DependencyClass(req.Spec, req.Operation))
}

func (a ChungbukTourAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("chungbuk-tour adapter call support is not enabled yet")
}
