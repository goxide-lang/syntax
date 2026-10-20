package enumbench

import (
	"math/rand"
	"testing"
	"unsafe"
)

const unitVariantCount = 7
const unitBatchSize = unitVariantCount * 1024

// Each variant appears exactly 1024 times in a reproducible shuffled order.
// Generation is outside the timed loop, and all representations reuse it.
func unitIndices() []uint8 {
	values := make([]uint8, unitBatchSize)
	for i := range values {
		values[i] = uint8(i % unitVariantCount)
	}
	random := rand.New(rand.NewSource(107))
	random.Shuffle(len(values), func(i, j int) { values[i], values[j] = values[j], values[i] })
	return values
}

func unitMustPanic(t *testing.T, call func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("invalid enum value did not panic")
		}
	}()
	call()
}

type unitUnknown struct{}

func (*unitUnknown) isUnitKind() {}

func TestUnitRepresentationBehavior(t *testing.T) {
	want := [...]uint64{17, 31, 47, 61, 79, 97, 109}
	for i, score := range want {
		numeric := MakeUnitNumeric(uint8(i))
		numericInt := MakeUnitNumericInt(uint8(i))
		boxed := MakeUnitKind(uint8(i))
		if got := UnitNumericScore(numeric); got != score {
			t.Errorf("numeric %d: got %d, want %d", i, got, score)
		}
		if got := UnitKindScore(boxed); got != score {
			t.Errorf("boxed %d: got %d, want %d", i, got, score)
		}
		if got := UnitNumericIntScore(numericInt); got != score {
			t.Errorf("numeric int %d: got %d, want %d", i, got, score)
		}
	}
	for _, invalid := range []uint8{7, 255} {
		unitMustPanic(t, func() { MakeUnitNumeric(invalid) })
		unitMustPanic(t, func() { MakeUnitNumericInt(invalid) })
		unitMustPanic(t, func() { MakeUnitKind(invalid) })
		unitMustPanic(t, func() { UnitNumericScore(UnitNumeric(invalid)) })
		unitMustPanic(t, func() { UnitNumericIntScore(UnitNumericInt(invalid)) })
	}
	unitMustPanic(t, func() { UnitNumericIntScore(-1) })
	// These paths are correctness checks, not timed valid-input workloads.
	// Empty struct pointer addresses are never used to distinguish variants.
	for _, invalid := range []UnitKind{
		nil, (*UnitIdent)(nil), (*UnitKeyword)(nil), (*UnitLiteral)(nil),
		(*UnitPunct)(nil), (*UnitSemicolon)(nil), (*UnitGroup)(nil), (*UnitFragment)(nil),
		&unitUnknown{},
	} {
		unitMustPanic(t, func() { UnitKindScore(invalid) })
	}
	var numericZero UnitNumeric
	var numericIntZero UnitNumericInt
	if UnitNumericScore(numericZero) != want[0] || UnitNumericIntScore(numericIntZero) != want[0] {
		t.Fatal("numeric zero must remain the first variant")
	}
}

func TestUnitWorkloadAndSizes(t *testing.T) {
	indices := unitIndices()
	var histogram [unitVariantCount]int
	var numericTotal, numericIntTotal, boxedTotal uint64
	for _, index := range indices {
		histogram[index]++
		numericTotal += UnitNumericScore(MakeUnitNumeric(index))
		numericIntTotal += UnitNumericIntScore(MakeUnitNumericInt(index))
		boxedTotal += UnitKindScore(MakeUnitKind(index))
	}
	for variant, count := range histogram {
		if count != unitBatchSize/unitVariantCount {
			t.Errorf("variant %d appears %d times", variant, count)
		}
	}
	if numericTotal != boxedTotal || numericIntTotal != boxedTotal {
		t.Fatalf("different workload results: %d, %d, %d", numericTotal, numericIntTotal, boxedTotal)
	}
	t.Logf("slot sizes: uint8 enum=%d, native int=%d, native uint=%d, interface enum=%d; empty variant=%d, pointer=%d bytes",
		unsafe.Sizeof(UnitNumeric(0)), unsafe.Sizeof(UnitNumericInt(0)), unsafe.Sizeof(uint(0)),
		unsafe.Sizeof(UnitKind(nil)), unsafe.Sizeof(UnitIdent{}), unsafe.Sizeof((*UnitIdent)(nil)))
}
