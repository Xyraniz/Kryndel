package kry

import (
	"math"
	"testing"
)

func TestFloatMathEdgeCases(t *testing.T) {
	values := []float64{math.SmallestNonzeroFloat64, math.MaxFloat64, math.Copysign(0, -1)}
	for _, value := range values {
		if !isFinite(value) {
			t.Fatalf("expected finite value %g", value)
		}
	}
	if math.Signbit(math.Copysign(0, -1)) == false {
		t.Fatal("negative zero lost its sign bit")
	}
}
