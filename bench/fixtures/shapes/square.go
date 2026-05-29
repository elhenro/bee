package shapes

type Square struct {
	Side float64
}

func (s Square) Area() float64 {
	return s.Side * s.Side
}
