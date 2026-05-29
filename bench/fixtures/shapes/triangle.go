package shapes

// Triangle carries explicit side lengths so perimeter is unambiguous.
type Triangle struct {
	Base, Height float64
	A, B, C      float64
}

func (t Triangle) Area() float64 {
	return 0.5 * t.Base * t.Height
}
