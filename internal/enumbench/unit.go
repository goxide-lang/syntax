package enumbench

// UnitNumeric has the same seven valid meanings as the generated UnitKind.
// Its native zero value is the first variant; UnitKind's zero value is nil and
// is rejected by generated match. Benchmarks use only valid constructed values.
type UnitNumeric uint8

const (
	UnitNumericIdent UnitNumeric = iota
	UnitNumericKeyword
	UnitNumericLiteral
	UnitNumericPunct
	UnitNumericSemicolon
	UnitNumericGroup
	UnitNumericFragment
)

func MakeUnitNumeric(index uint8) UnitNumeric {
	if index > uint8(UnitNumericFragment) {
		panic("enumbench: invalid unit kind")
	}
	return UnitNumeric(index)
}

func UnitNumericScore(value UnitNumeric) uint64 {
	switch value {
	case UnitNumericIdent:
		return 17
	case UnitNumericKeyword:
		return 31
	case UnitNumericLiteral:
		return 47
	case UnitNumericPunct:
		return 61
	case UnitNumericSemicolon:
		return 79
	case UnitNumericGroup:
		return 97
	case UnitNumericFragment:
		return 109
	default:
		panic("enumbench: invalid unit kind")
	}
}

// UnitNumericInt is the common native-int iota representation, alongside the
// deliberately compact uint8 baseline.
type UnitNumericInt int

const (
	UnitNumericIntIdent UnitNumericInt = iota
	UnitNumericIntKeyword
	UnitNumericIntLiteral
	UnitNumericIntPunct
	UnitNumericIntSemicolon
	UnitNumericIntGroup
	UnitNumericIntFragment
)

func MakeUnitNumericInt(index uint8) UnitNumericInt {
	if index > uint8(UnitNumericIntFragment) {
		panic("enumbench: invalid unit kind")
	}
	return UnitNumericInt(index)
}

func UnitNumericIntScore(value UnitNumericInt) uint64 {
	switch value {
	case UnitNumericIntIdent:
		return 17
	case UnitNumericIntKeyword:
		return 31
	case UnitNumericIntLiteral:
		return 47
	case UnitNumericIntPunct:
		return 61
	case UnitNumericIntSemicolon:
		return 79
	case UnitNumericIntGroup:
		return 97
	case UnitNumericIntFragment:
		return 109
	default:
		panic("enumbench: invalid unit kind")
	}
}
