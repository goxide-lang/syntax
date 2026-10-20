package enumbench

import (
	"testing"
	"unsafe"
)

// Run from the module root:
//
//	GOTOOLCHAIN=local GOWORK=off go test -run '^TestUnit' -bench '^BenchmarkUnit' -benchmem -count=5 ./internal/enumbench
//
// Construct and Dispatch process one value per operation. BatchBuild and
// Traversal process unitBatchSize values per operation and report ns/value.
// Construct converts a runtime category index into its representation: it
// includes the sealed factory's switch or the numeric validation/conversion.
// It does not measure a statically known variant literal's construction cost.
// B.Loop keeps calls/results live; sinks are assigned only after timing ends.
// All types retain their natural representations and invalid-value checks.
// In particular, no interface conversion is added to the numeric workload.
var (
	unitNumericSink      UnitNumeric
	unitNumericIntSink   UnitNumericInt
	unitBoxedSink        UnitKind
	unitValueSink        UnitValueKind
	unitChecksumSink     uint64
	unitNumericBatchSink []UnitNumeric
	unitIntBatchSink     []UnitNumericInt
	unitBoxedBatchSink   []UnitKind
	unitValueBatchSink   []UnitValueKind
)

func unitReport(b *testing.B, values int, width uintptr) {
	b.ReportMetric(float64(values), "values/op")
	b.ReportMetric(float64(width), "B/slot")
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(values), "ns/value")
}

func resetUnitBatchSinks(b *testing.B) {
	clear := func() {
		unitNumericBatchSink = nil
		unitIntBatchSink = nil
		unitBoxedBatchSink = nil
		unitValueBatchSink = nil
	}
	clear()
	b.Cleanup(clear)
}

func BenchmarkUnitConstruct(b *testing.B) {
	indices := unitIndices()
	b.Run("Numeric8", func(b *testing.B) {
		var result UnitNumeric
		position := 0
		b.ReportAllocs()
		for b.Loop() {
			result = MakeUnitNumeric(indices[position])
			position++
			if position == len(indices) {
				position = 0
			}
		}
		unitNumericSink = result
		unitReport(b, 1, unsafe.Sizeof(result))
	})
	b.Run("NumericInt", func(b *testing.B) {
		var result UnitNumericInt
		position := 0
		b.ReportAllocs()
		for b.Loop() {
			result = MakeUnitNumericInt(indices[position])
			position++
			if position == len(indices) {
				position = 0
			}
		}
		unitNumericIntSink = result
		unitReport(b, 1, unsafe.Sizeof(result))
	})
	b.Run("Sealed", func(b *testing.B) {
		var result UnitKind
		position := 0
		b.ReportAllocs()
		for b.Loop() {
			result = MakeUnitKind(indices[position])
			position++
			if position == len(indices) {
				position = 0
			}
		}
		unitBoxedSink = result
		unitReport(b, 1, unsafe.Sizeof(result))
	})
	b.Run("SealedValue", func(b *testing.B) {
		var result UnitValueKind
		position := 0
		b.ReportAllocs()
		for b.Loop() {
			result = MakeUnitValueKind(indices[position])
			position++
			if position == len(indices) {
				position = 0
			}
		}
		unitValueSink = result
		unitReport(b, 1, unsafe.Sizeof(result))
	})
}

func BenchmarkUnitDispatch(b *testing.B) {
	indices := unitIndices()
	b.Run("Numeric8", func(b *testing.B) {
		values := make([]UnitNumeric, len(indices))
		for i, index := range indices {
			values[i] = MakeUnitNumeric(index)
		}
		var total uint64
		position := 0
		b.ReportAllocs()
		for b.Loop() {
			total += UnitNumericScore(values[position])
			position++
			if position == len(values) {
				position = 0
			}
		}
		unitChecksumSink = total
		unitReport(b, 1, unsafe.Sizeof(UnitNumeric(0)))
	})
	b.Run("NumericInt", func(b *testing.B) {
		values := make([]UnitNumericInt, len(indices))
		for i, index := range indices {
			values[i] = MakeUnitNumericInt(index)
		}
		var total uint64
		position := 0
		b.ReportAllocs()
		for b.Loop() {
			total += UnitNumericIntScore(values[position])
			position++
			if position == len(values) {
				position = 0
			}
		}
		unitChecksumSink = total
		unitReport(b, 1, unsafe.Sizeof(UnitNumericInt(0)))
	})
	b.Run("Sealed", func(b *testing.B) {
		values := make([]UnitKind, len(indices))
		for i, index := range indices {
			values[i] = MakeUnitKind(index)
		}
		var total uint64
		position := 0
		b.ReportAllocs()
		for b.Loop() {
			total += UnitKindScore(values[position])
			position++
			if position == len(values) {
				position = 0
			}
		}
		unitChecksumSink = total
		unitReport(b, 1, unsafe.Sizeof(UnitKind(nil)))
	})
	b.Run("SealedValue", func(b *testing.B) {
		values := make([]UnitValueKind, len(indices))
		for i, index := range indices {
			values[i] = MakeUnitValueKind(index)
		}
		var total uint64
		position := 0
		b.ReportAllocs()
		for b.Loop() {
			total += UnitValueKindScore(values[position])
			position++
			if position == len(values) {
				position = 0
			}
		}
		unitChecksumSink = total
		unitReport(b, 1, unsafe.Sizeof(UnitValueKind(nil)))
	})
}

func BenchmarkUnitBatchBuild(b *testing.B) {
	indices := unitIndices()
	b.Run("Numeric8", func(b *testing.B) {
		resetUnitBatchSinks(b)
		var result []UnitNumeric
		b.ReportAllocs()
		for b.Loop() {
			values := make([]UnitNumeric, len(indices))
			for i, index := range indices {
				values[i] = MakeUnitNumeric(index)
			}
			result = values
		}
		unitNumericBatchSink = result
		unitReport(b, len(indices), unsafe.Sizeof(UnitNumeric(0)))
	})
	b.Run("NumericInt", func(b *testing.B) {
		resetUnitBatchSinks(b)
		var result []UnitNumericInt
		b.ReportAllocs()
		for b.Loop() {
			values := make([]UnitNumericInt, len(indices))
			for i, index := range indices {
				values[i] = MakeUnitNumericInt(index)
			}
			result = values
		}
		unitIntBatchSink = result
		unitReport(b, len(indices), unsafe.Sizeof(UnitNumericInt(0)))
	})
	b.Run("Sealed", func(b *testing.B) {
		resetUnitBatchSinks(b)
		var result []UnitKind
		b.ReportAllocs()
		for b.Loop() {
			values := make([]UnitKind, len(indices))
			for i, index := range indices {
				values[i] = MakeUnitKind(index)
			}
			result = values
		}
		unitBoxedBatchSink = result
		unitReport(b, len(indices), unsafe.Sizeof(UnitKind(nil)))
	})
	b.Run("SealedValue", func(b *testing.B) {
		resetUnitBatchSinks(b)
		var result []UnitValueKind
		b.ReportAllocs()
		for b.Loop() {
			values := make([]UnitValueKind, len(indices))
			for i, index := range indices {
				values[i] = MakeUnitValueKind(index)
			}
			result = values
		}
		unitValueBatchSink = result
		unitReport(b, len(indices), unsafe.Sizeof(UnitValueKind(nil)))
	})
}

func BenchmarkUnitTraversal(b *testing.B) {
	indices := unitIndices()
	b.Run("Numeric8", func(b *testing.B) {
		values := make([]UnitNumeric, len(indices))
		for i, index := range indices {
			values[i] = MakeUnitNumeric(index)
		}
		var total uint64
		b.ReportAllocs()
		for b.Loop() {
			for _, value := range values {
				total += UnitNumericScore(value)
			}
		}
		unitChecksumSink = total
		unitReport(b, len(indices), unsafe.Sizeof(UnitNumeric(0)))
	})
	b.Run("NumericInt", func(b *testing.B) {
		values := make([]UnitNumericInt, len(indices))
		for i, index := range indices {
			values[i] = MakeUnitNumericInt(index)
		}
		var total uint64
		b.ReportAllocs()
		for b.Loop() {
			for _, value := range values {
				total += UnitNumericIntScore(value)
			}
		}
		unitChecksumSink = total
		unitReport(b, len(indices), unsafe.Sizeof(UnitNumericInt(0)))
	})
	b.Run("Sealed", func(b *testing.B) {
		values := make([]UnitKind, len(indices))
		for i, index := range indices {
			values[i] = MakeUnitKind(index)
		}
		var total uint64
		b.ReportAllocs()
		for b.Loop() {
			for _, value := range values {
				total += UnitKindScore(value)
			}
		}
		unitChecksumSink = total
		unitReport(b, len(indices), unsafe.Sizeof(UnitKind(nil)))
	})
	b.Run("SealedValue", func(b *testing.B) {
		values := make([]UnitValueKind, len(indices))
		for i, index := range indices {
			values[i] = MakeUnitValueKind(index)
		}
		var total uint64
		b.ReportAllocs()
		for b.Loop() {
			for _, value := range values {
				total += UnitValueKindScore(value)
			}
		}
		unitChecksumSink = total
		unitReport(b, len(indices), unsafe.Sizeof(UnitValueKind(nil)))
	})
}
