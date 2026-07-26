package align

// Unsigned is a local type constraint for unsigned integers.
//
// We intentionally avoid external dependencies (e.g. golang.org/x/exp/constraints)
// to keep go-foundation stdlib-only.
type Unsigned interface {
	~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

func isPow2[T Unsigned](a T) bool {
	return a != 0 && (a&(a-1)) == 0
}

// Up aligns v up to the next multiple of a.
//
// a must be a power of two.
func Up[T Unsigned](v, a T) T {
	if !isPow2(a) {
		panic("align: alignment must be a power of two")
	}
	return (v + (a - 1)) &^ (a - 1)
}

// Down aligns v down to the previous multiple of a.
//
// a must be a power of two.
func Down[T Unsigned](v, a T) T {
	if !isPow2(a) {
		panic("align: alignment must be a power of two")
	}
	return v &^ (a - 1)
}
