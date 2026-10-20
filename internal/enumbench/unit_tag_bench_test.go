package enumbench

import (
	"testing"
	"unsafe"
)

// BenchmarkUnitDispatchStrategy changes order independently of representation.
// Shuffled and Grouped have identical variant counts. Single is monomorphic.
// Construction and conversion to the tagged interfaces occur before timing.
// All consumers use direct source calls; no function-valued callback or
// noinline annotation is added to any representation.
func BenchmarkUnitDispatchStrategy(b *testing.B) {
	for _, order := range []string{"Shuffled", "Grouped", "Single"} {
		b.Run(order, func(b *testing.B) {
			indices := dispatchIndices(order)
			b.Run("Numeric8", func(b *testing.B) {
				values := make([]UnitNumeric, len(indices))
				for i, index := range indices {
					values[i] = MakeUnitNumeric(index)
				}
				var total uint64
				for b.Loop() {
					for _, value := range values {
						total += UnitNumericScore(value)
					}
				}
				unitChecksumSink = total
				unitReport(b, len(values), unsafe.Sizeof(UnitNumeric(0)))
			})
			b.Run("TypeSwitchPointer", func(b *testing.B) {
				values := make([]UnitKind, len(indices))
				for i, index := range indices {
					values[i] = MakeUnitKind(index)
				}
				var total uint64
				for b.Loop() {
					for _, value := range values {
						total += UnitKindScore(value)
					}
				}
				unitChecksumSink = total
				unitReport(b, len(values), unsafe.Sizeof(UnitKind(nil)))
			})
			b.Run("TypeSwitchValue", func(b *testing.B) {
				values := make([]UnitValueKind, len(indices))
				for i, index := range indices {
					values[i] = MakeUnitValueKind(index)
				}
				var total uint64
				for b.Loop() {
					for _, value := range values {
						total += UnitValueKindScore(value)
					}
				}
				unitChecksumSink = total
				unitReport(b, len(values), unsafe.Sizeof(UnitValueKind(nil)))
			})
			b.Run("TypeMethodPointer", func(b *testing.B) {
				values := make([]UnitTaggedPointer, len(indices))
				for i, index := range indices {
					values[i] = MakeUnitKind(index).(UnitTaggedPointer)
				}
				var total uint64
				for b.Loop() {
					for _, value := range values {
						total += UnitTaggedPointerScore(value)
					}
				}
				unitChecksumSink = total
				unitReport(b, len(values), unsafe.Sizeof(UnitTaggedPointer(nil)))
			})
			b.Run("TypeMethodValue", func(b *testing.B) {
				values := make([]UnitTaggedValue, len(indices))
				for i, index := range indices {
					values[i] = MakeUnitValueKind(index).(UnitTaggedValue)
				}
				var total uint64
				for b.Loop() {
					for _, value := range values {
						total += UnitTaggedValueScore(value)
					}
				}
				unitChecksumSink = total
				unitReport(b, len(values), unsafe.Sizeof(UnitTaggedValue(nil)))
			})
		})
	}
}
