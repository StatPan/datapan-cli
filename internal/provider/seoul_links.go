package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type SeoulTDataAdapter struct {
	StaticHostMatcher
}

func NewSeoulTDataAdapter() SeoulTDataAdapter {
	return SeoulTDataAdapter{StaticHostMatcher{Hosts: SeoulTDataHosts()}}
}

func SeoulTDataHosts() []string {
	return []string{"t-data.seoul.go.kr"}
}

func (a SeoulTDataAdapter) Name() string { return "seoul-tdata" }

func (a SeoulTDataAdapter) Hosts() []string { return SeoulTDataHosts() }

func (a SeoulTDataAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a SeoulTDataAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "seoul-tdata", a.DependencyClass(req.Spec, req.Operation))
}

func (a SeoulTDataAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("seoul-tdata adapter call support is not enabled yet")
}

type SeoulMapAdapter struct {
	StaticHostMatcher
}

func NewSeoulMapAdapter() SeoulMapAdapter {
	return SeoulMapAdapter{StaticHostMatcher{Hosts: SeoulMapHosts()}}
}

func SeoulMapHosts() []string {
	return []string{"map.seoul.go.kr"}
}

func (a SeoulMapAdapter) Name() string { return "seoul-map" }

func (a SeoulMapAdapter) Hosts() []string { return SeoulMapHosts() }

func (a SeoulMapAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a SeoulMapAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "seoul-map", a.DependencyClass(req.Spec, req.Operation))
}

func (a SeoulMapAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("seoul-map adapter call support is not enabled yet")
}

type JongnoAdapter struct {
	StaticHostMatcher
}

func NewJongnoAdapter() JongnoAdapter {
	return JongnoAdapter{StaticHostMatcher{Hosts: JongnoHosts()}}
}

func JongnoHosts() []string {
	return []string{
		"openapi.jongno.go.kr",
		"openapi.jongno.go.kr:8088",
	}
}

func (a JongnoAdapter) Name() string { return "jongno" }

func (a JongnoAdapter) Hosts() []string { return JongnoHosts() }

func (a JongnoAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a JongnoAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "jongno", a.DependencyClass(req.Spec, req.Operation))
}

func (a JongnoAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("jongno adapter call support is not enabled yet")
}

type KOSMESAdapter struct {
	StaticHostMatcher
}

func NewKOSMESAdapter() KOSMESAdapter {
	return KOSMESAdapter{StaticHostMatcher{Hosts: KOSMESHosts()}}
}

func KOSMESHosts() []string {
	return []string{"kosmes.or.kr"}
}

func (a KOSMESAdapter) Name() string { return "kosmes" }

func (a KOSMESAdapter) Hosts() []string { return KOSMESHosts() }

func (a KOSMESAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a KOSMESAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "kosmes", a.DependencyClass(req.Spec, req.Operation))
}

func (a KOSMESAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("kosmes adapter call support is not enabled yet")
}
