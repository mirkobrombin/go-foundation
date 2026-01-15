package options

// Option is a functional option for configuration.
type Option[T any] func(*T)

// Apply applies functional options to a config struct.
//
// Example:
//
//	cfg := &Config{}
//	options.Apply(cfg, WithHost("localhost"))
func Apply[T any](cfg *T, opts ...Option[T]) {
	for _, opt := range opts {
		opt(cfg)
	}
}

// Builder provides a fluent interface for building config with options.
//
// Example:
//
//	cfg := options.NewBuilder(defaultCfg).
//		With(WithHost("localhost")).
//		Build()
type Builder[T any] struct {
	cfg  T
	opts []Option[T]
}

// NewBuilder creates a new options builder with default config.
func NewBuilder[T any](defaults T) *Builder[T] {
	return &Builder[T]{cfg: defaults}
}

// With adds an option to the builder.
//
// Returns:
//
// The builder instance for chaining.
func (b *Builder[T]) With(opt Option[T]) *Builder[T] {
	b.opts = append(b.opts, opt)
	return b
}

// Build applies all options and returns the final config.
func (b *Builder[T]) Build() T {
	Apply(&b.cfg, b.opts...)
	return b.cfg
}

// Ptr returns a pointer to the built config.
func (b *Builder[T]) Ptr() *T {
	Apply(&b.cfg, b.opts...)
	return &b.cfg
}
