package reflectutil

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Bind converts a string to a reflect.Value based on its kind.
//
// It supports basic types (string, int, uint, float, bool, duration) and slices of strings.
//
// Example:
//
//	var x int
//	err := reflectutil.Bind(reflect.ValueOf(&x).Elem(), "42")
func Bind(val reflect.Value, value string) error {
	if !val.CanSet() {
		return fmt.Errorf("reflectutil: cannot set value")
	}

	switch val.Kind() {
	case reflect.String:
		val.SetString(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if val.Type() == reflect.TypeOf(time.Duration(0)) {
			d, err := time.ParseDuration(value)
			if err != nil {
				return fmt.Errorf("reflectutil: invalid duration: %v", value)
			}
			val.SetInt(int64(d))
		} else {
			i, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return fmt.Errorf("reflectutil: invalid integer: %v", value)
			}
			val.SetInt(i)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return fmt.Errorf("reflectutil: invalid unsigned integer: %v", value)
		}
		val.SetUint(u)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("reflectutil: invalid float: %v", value)
		}
		val.SetFloat(f)
	case reflect.Bool:
		b, err := ParseBoolExtended(value)
		if err != nil {
			return fmt.Errorf("reflectutil: %w", err)
		}
		val.SetBool(b)
	case reflect.Slice:
		if val.Type().Elem().Kind() == reflect.String {
			val.Set(reflect.Append(val, reflect.ValueOf(value)))
		} else {
			return fmt.Errorf("reflectutil: unsupported slice type: %v", val.Type().Elem().Kind())
		}
	default:
		return fmt.Errorf("reflectutil: unsupported type: %v", val.Kind())
	}
	return nil
}

// ParseBoolExtended parses a boolean string with support for more formats
// than strconv.ParseBool.
//
// It accepts 1, t, true, yes, y, on as true.
// It accepts 0, f, false, no, n, off as false.
func ParseBoolExtended(str string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(str)) {
	case "1", "t", "true", "yes", "y", "on":
		return true, nil
	case "0", "f", "false", "no", "n", "off":
		return false, nil
	}
	return false, fmt.Errorf("invalid boolean value: %s", str)
}

// BindStruct populates a struct from a map[string]string using `conf` tags or field names.
//
// Example:
//
//	type Config struct {
//		Name  string `conf:"name"`
//		Count int    `conf:"count"`
//	}
//	var cfg Config
//	err := reflectutil.BindStruct(&cfg, map[string]string{"name": "test", "count": "42"})
func BindStruct(dst any, src map[string]string) error {
	val := reflect.ValueOf(dst)
	if val.Kind() != reflect.Ptr || val.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("reflectutil: dst must be a pointer to a struct")
	}

	elem := val.Elem()
	typ := elem.Type()

	for i := 0; i < elem.NumField(); i++ {
		field := elem.Field(i)
		if !field.CanSet() {
			continue
		}

		fieldType := typ.Field(i)
		tag := fieldType.Tag.Get("conf")
		if tag == "" {
			tag = fieldType.Name
		}

		if v, ok := src[tag]; ok {
			if err := Bind(field, v); err != nil {
				return fmt.Errorf("reflectutil: field %s: %w", fieldType.Name, err)
			}
		}
	}
	return nil
}