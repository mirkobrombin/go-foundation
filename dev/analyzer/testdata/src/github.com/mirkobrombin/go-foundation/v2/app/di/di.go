package di

type Builder struct{}

func NewBuilder() *Builder {
	return &Builder{}
}

func RegisterImpl[I, T any](*Builder) {}

func RegisterAs[I, T any](*Builder, func() T) {}

func RegisterFromFunc[T any](*Builder, any) {}

func (b *Builder) Provide(string, any) {}
