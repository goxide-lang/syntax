package enumbench

// These experimental interfaces dispatch on a method's returned tag. A tag is
// not a proof of the dynamic type: payload access would still need an assertion.
// The benchmark uses only the seven variants created by the matching factories.
type UnitTaggedPointer interface {
	UnitKind
	Type() UnitNumeric
}

type UnitTaggedValue interface {
	UnitValueKind
	Type() UnitNumeric
}

// Preserve the generated enum's rejection of typed nil pointers. Returning a
// constant without these checks would silently make typed nil a valid variant.
func (value *UnitIdent) Type() UnitNumeric {
	if value == nil {
		panic("enumbench: nil unit kind")
	}
	return UnitNumericIdent
}

func (value *UnitKeyword) Type() UnitNumeric {
	if value == nil {
		panic("enumbench: nil unit kind")
	}
	return UnitNumericKeyword
}

func (value *UnitLiteral) Type() UnitNumeric {
	if value == nil {
		panic("enumbench: nil unit kind")
	}
	return UnitNumericLiteral
}

func (value *UnitPunct) Type() UnitNumeric {
	if value == nil {
		panic("enumbench: nil unit kind")
	}
	return UnitNumericPunct
}

func (value *UnitSemicolon) Type() UnitNumeric {
	if value == nil {
		panic("enumbench: nil unit kind")
	}
	return UnitNumericSemicolon
}

func (value *UnitGroup) Type() UnitNumeric {
	if value == nil {
		panic("enumbench: nil unit kind")
	}
	return UnitNumericGroup
}

func (value *UnitFragment) Type() UnitNumeric {
	if value == nil {
		panic("enumbench: nil unit kind")
	}
	return UnitNumericFragment
}

func (UnitValueIdent) Type() UnitNumeric     { return UnitNumericIdent }
func (UnitValueKeyword) Type() UnitNumeric   { return UnitNumericKeyword }
func (UnitValueLiteral) Type() UnitNumeric   { return UnitNumericLiteral }
func (UnitValuePunct) Type() UnitNumeric     { return UnitNumericPunct }
func (UnitValueSemicolon) Type() UnitNumeric { return UnitNumericSemicolon }
func (UnitValueGroup) Type() UnitNumeric     { return UnitNumericGroup }
func (UnitValueFragment) Type() UnitNumeric  { return UnitNumericFragment }

func UnitTaggedPointerScore(value UnitTaggedPointer) uint64 {
	if value == nil {
		panic("enumbench: nil unit kind")
	}
	return UnitNumericScore(value.Type())
}

func UnitTaggedValueScore(value UnitTaggedValue) uint64 {
	if value == nil {
		panic("enumbench: nil unit kind")
	}
	return UnitNumericScore(value.Type())
}
