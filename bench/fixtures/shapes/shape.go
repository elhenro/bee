package shapes

// Shape is implemented by every concrete shape in this package.
type Shape interface {
	Area() float64
}

// Registry holds shapes so callers can sum metrics across all of them.
type Registry struct {
	shapes []Shape
}

func NewRegistry() *Registry {
	return &Registry{shapes: []Shape{
		Rectangle{W: 2, H: 3},
		Circle{R: 1},
		Triangle{Base: 4, Height: 2, A: 3, B: 4, C: 5},
		Square{Side: 5},
	}}
}

// TotalArea sums the area of every registered shape.
func (r *Registry) TotalArea() float64 {
	var sum float64
	for _, s := range r.shapes {
		sum += s.Area()
	}
	return sum
}
