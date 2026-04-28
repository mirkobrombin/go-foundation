package checker

import (
	"fmt"
	"reflect"
)

// IsMatch checks if the value of a field matches the user ID.
func IsMatch(val reflect.Value, userID string) bool {
	switch val.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < val.Len(); i++ {
			if IsMatch(val.Index(i), userID) {
				return true
			}
		}
		return false
	case reflect.Map:
		for _, key := range val.MapKeys() {
			if IsMatch(key, userID) {
				return true
			}
		}
		return false
	case reflect.String:
		return val.String() == userID
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprintf("%d", val.Int()) == userID
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return fmt.Sprintf("%d", val.Uint()) == userID
	case reflect.Ptr, reflect.Interface:
		if val.IsNil() {
			return false
		}
		return IsMatch(val.Elem(), userID)
	default:
		if s, ok := val.Interface().(fmt.Stringer); ok {
			return s.String() == userID
		}
		return fmt.Sprintf("%v", val.Interface()) == userID
	}
}
