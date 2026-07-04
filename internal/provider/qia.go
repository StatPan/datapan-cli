package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type QIAAdapter struct {
	StaticHostMatcher
}

func NewQIAAdapter() QIAAdapter {
	return QIAAdapter{StaticHostMatcher{Hosts: QIAHosts()}}
}

func QIAHosts() []string {
	return []string{
		"home.kahis.go.kr",
		"meatwatch.go.kr",
	}
}

func (a QIAAdapter) Name() string { return "qia" }

func (a QIAAdapter) Hosts() []string { return QIAHosts() }

func (a QIAAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a QIAAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "qia", a.DependencyClass(req.Spec, req.Operation))
}

func (a QIAAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("qia adapter call support is not enabled yet")
}
