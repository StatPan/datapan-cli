package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type OpenFiscalDataAdapter struct {
	StaticHostMatcher
}

func NewOpenFiscalDataAdapter() OpenFiscalDataAdapter {
	return OpenFiscalDataAdapter{StaticHostMatcher{Hosts: OpenFiscalDataHosts()}}
}

func OpenFiscalDataHosts() []string {
	return []string{"www.openfiscaldata.go.kr"}
}

func (a OpenFiscalDataAdapter) Name() string { return "openfiscaldata" }

func (a OpenFiscalDataAdapter) Hosts() []string { return OpenFiscalDataHosts() }

func (a OpenFiscalDataAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a OpenFiscalDataAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "openfiscaldata", a.DependencyClass(req.Spec, req.Operation))
}

func (a OpenFiscalDataAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("openfiscaldata adapter call support is not enabled yet")
}
