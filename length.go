package gositter

// length.h and point.h.

// Length
type length struct {
	bytes  uint32
	extent Point
}

// LENGTH_UNDEFINED
var lengthUndefined = length{0, Point{0, 1}}

// length_is_undefined
func lengthIsUndefined(l length) bool {
	return l.bytes == 0 && l.extent.Column != 0
}

// point_add
func pointAdd(a, b Point) Point {
	if b.Row > 0 {
		return Point{a.Row + b.Row, b.Column}
	}
	return Point{a.Row, a.Column + b.Column}
}

// point_sub
func pointSub(a, b Point) Point {
	if a.Row > b.Row {
		return Point{a.Row - b.Row, a.Column}
	}
	if a.Column >= b.Column {
		return Point{0, a.Column - b.Column}
	}
	return Point{0, 0}
}

// length_add
func lengthAdd(a, b length) length {
	return length{a.bytes + b.bytes, pointAdd(a.extent, b.extent)}
}

// length_sub
func lengthSub(a, b length) length {
	var r length
	if a.bytes >= b.bytes {
		r.bytes = a.bytes - b.bytes
	}
	r.extent = pointSub(a.extent, b.extent)
	return r
}
