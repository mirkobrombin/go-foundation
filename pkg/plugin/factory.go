package plugin

// FactoryRegistry stores plugin factories by name.
type FactoryRegistry struct {
	byName map[string]Factory
}

// NewFactoryRegistry creates an empty factory registry.
func NewFactoryRegistry() *FactoryRegistry {
	return &FactoryRegistry{byName: make(map[string]Factory)}
}

// Register stores a named factory.
func (f *FactoryRegistry) Register(name string, factory Factory) error {
	if _, ok := f.byName[name]; ok {
		return ErrFactoryExists
	}
	f.byName[name] = factory
	return nil
}

// Create builds a plugin from a registered factory.
func (f *FactoryRegistry) Create(name string) (Plugin, error) {
	factory, ok := f.byName[name]
	if !ok {
		return nil, ErrFactoryNotFound
	}
	return factory(), nil
}