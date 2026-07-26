package plugin

import (
	"errors"
	stdplugin "plugin"
)

// LoadSo loads a Go plugin (.so) exposing a PluginFactory symbol.
func LoadSo(path string) (Factory, error) {
	p, err := stdplugin.Open(path)
	if err != nil {
		return nil, err
	}

	symbol, err := p.Lookup("PluginFactory")
	if err != nil {
		return nil, err
	}

	if factory, ok := symbol.(func() Plugin); ok {
		return factory, nil
	}
	if factory, ok := symbol.(*func() Plugin); ok {
		return *factory, nil
	}
	return nil, errors.New("plugin: PluginFactory has wrong type")
}
