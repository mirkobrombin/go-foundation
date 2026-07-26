package ring

// ByteBuffer is a ring buffer specialized for bytes.
//
// Semantics match Buffer[T]: it keeps one slot empty internally.
type ByteBuffer struct {
	buf []byte
	r   int
	w   int
}

// NewBytes creates a new ByteBuffer with the given capacity.
func NewBytes(capacity int) *ByteBuffer {
	if capacity < 1 {
		panic("ring: capacity must be >= 1")
	}
	return &ByteBuffer{buf: make([]byte, capacity+1)}
}

func (b *ByteBuffer) empty() bool { return b.r == b.w }

func (b *ByteBuffer) Cap() int {
	if b == nil {
		return 0
	}
	return len(b.buf) - 1
}

func (b *ByteBuffer) Len() int {
	if b == nil {
		return 0
	}
	if b.w >= b.r {
		return b.w - b.r
	}
	return len(b.buf) - b.r + b.w
}

func (b *ByteBuffer) Space() int { return b.Cap() - b.Len() }

func (b *ByteBuffer) Reset() { b.r, b.w = 0, 0 }

// Write writes as many bytes as possible and returns the number of bytes written.
func (b *ByteBuffer) Write(p []byte) int {
	if b == nil || len(p) == 0 {
		return 0
	}
	toWrite := min(b.Space(), len(p))
	if toWrite == 0 {
		return 0
	}

	// Copy in up to two chunks.
	first := min(len(b.buf)-b.w, toWrite)
	copy(b.buf[b.w:b.w+first], p[:first])
	b.w = (b.w + first) % len(b.buf)

	second := toWrite - first
	if second > 0 {
		copy(b.buf[b.w:b.w+second], p[first:first+second])
		b.w += second
	}
	return toWrite
}

// Read reads up to len(p) bytes and returns the number of bytes read.
func (b *ByteBuffer) Read(p []byte) int {
	if b == nil || len(p) == 0 {
		return 0
	}
	toRead := min(b.Len(), len(p))
	if toRead == 0 {
		return 0
	}

	first := min(len(b.buf)-b.r, toRead)
	copy(p[:first], b.buf[b.r:b.r+first])
	b.r = (b.r + first) % len(b.buf)

	second := toRead - first
	if second > 0 {
		copy(p[first:first+second], b.buf[b.r:b.r+second])
		b.r += second
	}
	return toRead
}
