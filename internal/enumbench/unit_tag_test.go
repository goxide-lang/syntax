package enumbench

import "testing"

type invalidPointerTag struct{ UnitKind }

func (invalidPointerTag) Type() UnitNumeric { return 255 }

type invalidValueTag struct{ UnitValueKind }

func (invalidValueTag) Type() UnitNumeric { return 255 }

func TestUnitTagDispatch(t *testing.T) {
	for index := range uint8(unitVariantCount) {
		want := UnitNumericScore(MakeUnitNumeric(index))
		pointer := MakeUnitKind(index).(UnitTaggedPointer)
		value := MakeUnitValueKind(index).(UnitTaggedValue)
		if got := UnitTaggedPointerScore(pointer); got != want {
			t.Errorf("pointer tag %d: got %d, want %d", index, got, want)
		}
		if got := UnitTaggedValueScore(value); got != want {
			t.Errorf("value tag %d: got %d, want %d", index, got, want)
		}
		if pointer.Type() != UnitNumeric(index) || value.Type() != UnitNumeric(index) {
			t.Errorf("wrong tag for variant %d", index)
		}
	}
	for _, invalid := range []UnitTaggedPointer{
		nil, invalidPointerTag{}, (*UnitIdent)(nil), (*UnitKeyword)(nil),
		(*UnitLiteral)(nil), (*UnitPunct)(nil), (*UnitSemicolon)(nil),
		(*UnitGroup)(nil), (*UnitFragment)(nil),
	} {
		unitMustPanic(t, func() { UnitTaggedPointerScore(invalid) })
	}
	for _, invalid := range []UnitTaggedValue{
		nil, invalidValueTag{}, (*UnitValueIdent)(nil), (*UnitValueKeyword)(nil),
		(*UnitValueLiteral)(nil), (*UnitValuePunct)(nil), (*UnitValueSemicolon)(nil),
		(*UnitValueGroup)(nil), (*UnitValueFragment)(nil),
	} {
		unitMustPanic(t, func() { UnitTaggedValueScore(invalid) })
	}
}

func dispatchIndices(order string) []uint8 {
	values := make([]uint8, unitBatchSize)
	switch order {
	case "Shuffled":
		return unitIndices()
	case "Grouped":
		for i := range values {
			values[i] = uint8(i / (unitBatchSize / unitVariantCount))
		}
	case "Single":
		for i := range values {
			values[i] = 3
		}
	default:
		panic("enumbench: invalid dispatch order")
	}
	return values
}

func TestDispatchOrders(t *testing.T) {
	for _, order := range []string{"Shuffled", "Grouped", "Single"} {
		indices := dispatchIndices(order)
		if len(indices) != unitBatchSize {
			t.Fatalf("%s has %d elements", order, len(indices))
		}
		var counts [unitVariantCount]int
		for _, index := range indices {
			counts[index]++
			want := UnitNumericScore(MakeUnitNumeric(index))
			if UnitKindScore(MakeUnitKind(index)) != want ||
				UnitValueKindScore(MakeUnitValueKind(index)) != want ||
				UnitTaggedPointerScore(MakeUnitKind(index).(UnitTaggedPointer)) != want ||
				UnitTaggedValueScore(MakeUnitValueKind(index).(UnitTaggedValue)) != want {
				t.Fatalf("%s has different dispatch results for %d", order, index)
			}
		}
		for index, count := range counts {
			want := unitBatchSize / unitVariantCount
			if order == "Single" {
				want = 0
				if index == 3 {
					want = unitBatchSize
				}
			}
			if count != want {
				t.Errorf("%s variant %d count=%d, want %d", order, index, count, want)
			}
		}
	}
}
