package align

import "testing"

func TestUpDown(t *testing.T) {
	if got := Up[uint64](0, 4); got != 0 {
		t.Fatalf("Up(0,4)=%d", got)
	}
	if got := Up[uint64](1, 4); got != 4 {
		t.Fatalf("Up(1,4)=%d", got)
	}
	if got := Up[uint64](4, 4); got != 4 {
		t.Fatalf("Up(4,4)=%d", got)
	}
	if got := Down[uint64](5, 4); got != 4 {
		t.Fatalf("Down(5,4)=%d", got)
	}
	if got := Down[uintptr](uintptr(0x1234), 0x100); got != 0x1200 {
		t.Fatalf("Down(0x1234,0x100)=0x%x", got)
	}
}

func TestPanicsOnInvalidAlign(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	_ = Up[uint64](1, 3)
}
