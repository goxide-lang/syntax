package enumbench

// UnitValueKind is a handwritten experiment. Goxide currently emits pointer
// variants; this comparison does not change that language representation.
type UnitValueKind interface {
	isUnitValueKind()
}

type UnitValueIdent struct{}
type UnitValueKeyword struct{}
type UnitValueLiteral struct{}
type UnitValuePunct struct{}
type UnitValueSemicolon struct{}
type UnitValueGroup struct{}
type UnitValueFragment struct{}

func (UnitValueIdent) isUnitValueKind()     {}
func (UnitValueKeyword) isUnitValueKind()   {}
func (UnitValueLiteral) isUnitValueKind()   {}
func (UnitValuePunct) isUnitValueKind()     {}
func (UnitValueSemicolon) isUnitValueKind() {}
func (UnitValueGroup) isUnitValueKind()     {}
func (UnitValueFragment) isUnitValueKind()  {}

func MakeUnitValueKind(index uint8) UnitValueKind {
	switch index {
	case 0:
		return UnitValueIdent{}
	case 1:
		return UnitValueKeyword{}
	case 2:
		return UnitValueLiteral{}
	case 3:
		return UnitValuePunct{}
	case 4:
		return UnitValueSemicolon{}
	case 5:
		return UnitValueGroup{}
	case 6:
		return UnitValueFragment{}
	default:
		panic("enumbench: invalid unit kind")
	}
}

func UnitValueKindScore(value UnitValueKind) uint64 {
	if value == nil {
		panic("enumbench: nil unit kind")
	}
	switch value.(type) {
	case UnitValueIdent:
		return 17
	case UnitValueKeyword:
		return 31
	case UnitValueLiteral:
		return 47
	case UnitValuePunct:
		return 61
	case UnitValueSemicolon:
		return 79
	case UnitValueGroup:
		return 97
	case UnitValueFragment:
		return 109
	default:
		panic("enumbench: invalid unit kind")
	}
}
