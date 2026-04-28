package plugin

import "reflect"

// DiscoverFromValues inspects provided values and registers any that implement Plugin.
func (r *Registry) DiscoverFromValues(values ...interface{}) int {
	if r == nil {
		return 0
	}

	pluginType := reflect.TypeOf((*Plugin)(nil)).Elem()
	count := 0
	for _, value := range values {
		if value == nil {
			continue
		}

		t := reflect.TypeOf(value)
		if t.Implements(pluginType) {
			p, _ := value.(Plugin)
			if p != nil && r.Register(p) == nil {
				count++
			}
			continue
		}

		if t.Kind() == reflect.Ptr && t.Elem().Implements(pluginType) {
			pluginValue, _ := reflect.ValueOf(value).Elem().Interface().(Plugin)
			if pluginValue != nil && r.Register(pluginValue) == nil {
				count++
			}
		}
	}
	return count
}
