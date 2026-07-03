package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type MNDOpenDataAdapter struct {
	StaticHostMatcher
}

func NewMNDOpenDataAdapter() MNDOpenDataAdapter {
	return MNDOpenDataAdapter{StaticHostMatcher{Hosts: MNDOpenDataHosts()}}
}

func MNDOpenDataHosts() []string {
	return []string{"opendata.mnd.go.kr"}
}

func (a MNDOpenDataAdapter) Name() string { return "mnd-open-data" }

func (a MNDOpenDataAdapter) Hosts() []string { return MNDOpenDataHosts() }

func (a MNDOpenDataAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a MNDOpenDataAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "mnd-open-data", a.DependencyClass(req.Spec, req.Operation))
}

func (a MNDOpenDataAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("mnd-open-data adapter call support is not enabled yet")
}
