package provider

import (
	"context"
	"fmt"

	"github.com/StatPan/datapan-cli/internal/datago"
)

type FoodSafetyKoreaAdapter struct {
	StaticHostMatcher
}

func NewFoodSafetyKoreaAdapter() FoodSafetyKoreaAdapter {
	return FoodSafetyKoreaAdapter{StaticHostMatcher{Hosts: FoodSafetyKoreaHosts()}}
}

func FoodSafetyKoreaHosts() []string {
	return []string{"www.foodsafetykorea.go.kr"}
}

func (a FoodSafetyKoreaAdapter) Name() string { return "foodsafetykorea" }

func (a FoodSafetyKoreaAdapter) Hosts() []string { return FoodSafetyKoreaHosts() }

func (a FoodSafetyKoreaAdapter) DependencyClass(spec datago.Spec, op datago.Operation) string {
	return datago.OperationDependencyClass(spec, op)
}

func (a FoodSafetyKoreaAdapter) Verify(ctx context.Context, req VerificationRequest) datago.VerificationResult {
	return verifyHTMLLandingPage(ctx, req, "foodsafetykorea", a.DependencyClass(req.Spec, req.Operation))
}

func (a FoodSafetyKoreaAdapter) Call(ctx context.Context, req CallRequest) (datago.ResponseEnvelope, error) {
	return datago.ResponseEnvelope{}, fmt.Errorf("foodsafetykorea adapter call support is not enabled yet")
}
