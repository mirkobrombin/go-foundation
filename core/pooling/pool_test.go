package pooling

import "testing"

func TestPoolReusesReturnedItem(t *testing.T) {
	next := 0
	p := New(func() int {
		next++
		return next
	}, WithMaxSize[int](1))

	item := p.Get()
	p.Put(item)

	if got := p.Get(); got != item {
		t.Fatalf("Get() = %d, want %d", got, item)
	}
}

func TestPoolMaxSizeLimitsRetainedItems(t *testing.T) {
	next := 0
	p := New(func() int {
		next++
		return next
	}, WithMaxSize[int](1))

	p.Put(10)
	p.Put(20)

	if got := p.Get(); got != 10 {
		t.Fatalf("Get() = %d, want first retained item", got)
	}
	if got := p.Get(); got != 1 {
		t.Fatalf("Get() = %d, want new factory item", got)
	}
}

func TestPoolFinalizerRunsBeforeRetain(t *testing.T) {
	p := New(func() []int { return []int{1, 2} }, WithFinalizer(func(v []int) {
		v[0] = 9
	}), WithMaxSize[[]int](1))

	item := p.Get()
	p.Put(item)

	got := p.Get()
	if got[0] != 9 {
		t.Fatalf("Get()[0] = %d, want finalizer mutation", got[0])
	}
}
