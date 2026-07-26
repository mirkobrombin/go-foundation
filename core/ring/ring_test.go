package ring

import (
	"bytes"
	"testing"
)

func TestBufferPushPopWrap(t *testing.T) {
	b := New[int](3)
	if b.Cap() != 3 {
		t.Fatalf("Cap=%d", b.Cap())
	}

	if !b.Push(1) || !b.Push(2) || !b.Push(3) {
		t.Fatal("expected push to succeed")
	}
	if b.Push(4) {
		t.Fatal("expected push to fail when full")
	}

	v, ok := b.Pop()
	if !ok || v != 1 {
		t.Fatalf("Pop=%v,%v", v, ok)
	}
	if !b.Push(4) {
		t.Fatal("expected push to succeed after pop")
	}

	// Now should read 2,3,4
	for _, want := range []int{2, 3, 4} {
		got, ok := b.Pop()
		if !ok || got != want {
			t.Fatalf("got %v ok=%v want %v", got, ok, want)
		}
	}
	if _, ok := b.Pop(); ok {
		t.Fatal("expected empty")
	}
}

func TestByteBufferReadWrite(t *testing.T) {
	b := NewBytes(5)
	in := []byte("hello")
	if n := b.Write(in); n != 5 {
		t.Fatalf("Write=%d", n)
	}
	if n := b.Write([]byte("!")); n != 0 {
		t.Fatalf("Write when full=%d", n)
	}

	out := make([]byte, 3)
	if n := b.Read(out); n != 3 {
		t.Fatalf("Read=%d", n)
	}
	if !bytes.Equal(out, []byte("hel")) {
		t.Fatalf("out=%q", out)
	}

	if n := b.Write([]byte("!!")); n != 2 {
		t.Fatalf("Write=%d", n)
	}

	out2 := make([]byte, 4)
	if n := b.Read(out2); n != 4 {
		t.Fatalf("Read=%d", n)
	}
	if !bytes.Equal(out2, []byte("lo!!")) {
		t.Fatalf("out2=%q", out2)
	}
}
