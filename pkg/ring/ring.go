package ring

// Buffer is a non-thread-safe ring (circular) buffer.
//
// Notes:
// - Capacity is the maximum number of elements it can hold.
// - Internally it keeps one slot empty to distinguish full vs empty.
// - This is intended as a low-level primitive; add locking externally if needed.
type Buffer[T any] struct {
	buf []T
	r   int
	w   int
}

// New creates a new ring buffer with the given capacity.
func New[T any](capacity int) *Buffer[T] {
	if capacity < 1 {
		panic("ring: capacity must be >= 1")
	}
	return &Buffer[T]{buf: make([]T, capacity+1)}
}

func (b *Buffer[T]) empty() bool { return b.r == b.w }
func (b *Buffer[T]) full() bool  { return (b.w+1)%len(b.buf) == b.r }

// Cap returns the buffer capacity.
func (b *Buffer[T]) Cap() int {
	if b == nil {
		return 0
	}
	return len(b.buf) - 1
}

// Len returns the number of elements currently stored.
func (b *Buffer[T]) Len() int {
	if b == nil {
		return 0
	}
	if b.w >= b.r {
		return b.w - b.r
	}
	return len(b.buf) - b.r + b.w
}

// Space returns the remaining free slots.
func (b *Buffer[T]) Space() int { return b.Cap() - b.Len() }

// Reset clears the buffer.
func (b *Buffer[T]) Reset() {
	b.r, b.w = 0, 0
}

// Push appends one element. Returns false if the buffer is full.
func (b *Buffer[T]) Push(v T) bool {
	if b == nil || b.full() {
		return false
	}
	b.buf[b.w] = v
	b.w = (b.w + 1) % len(b.buf)
	return true
}

// Pop removes and returns one element. ok is false if the buffer is empty.
func (b *Buffer[T]) Pop() (v T, ok bool) {
	if b == nil || b.empty() {
		return v, false
	}
	v = b.buf[b.r]
	b.r = (b.r + 1) % len(b.buf)
	return v, true
}

// Peek returns the next element without removing it. ok is false if the buffer is empty.
func (b *Buffer[T]) Peek() (v T, ok bool) {
	if b == nil || b.empty() {
		return v, false
	}
	return b.buf[b.r], true
}

// Drain returns all elements as a slice and resets the buffer.
func (b *Buffer[T]) Drain() []T {
	if b == nil || b.empty() {
		return nil
	}
	result := make([]T, 0, b.Len())
	for {
		v, ok := b.Pop()
		if !ok {
			break
		}
		result = append(result, v)
	}
	return result
}
