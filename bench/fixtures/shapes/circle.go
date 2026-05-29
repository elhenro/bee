package shapes

const pi = 3.141592653589793

type Circle struct {
	R float64
}

func (c Circle) Area() float64 {
	return pi * c.R * c.R
}
