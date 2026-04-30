package bind

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
)

// SourceFunc extracts a string value by key from a request source.
type SourceFunc func(key string) string

// Binder populates struct fields from path, query, header, and default sources.
type Binder struct {
	sources []source
}

type source struct {
	tag string
	fn  SourceFunc
}

// New creates an empty Binder.
func New() *Binder {
	return &Binder{}
}

// FromPath adds a path parameter source.
func (b *Binder) FromPath(fn SourceFunc) *Binder {
	b.sources = append(b.sources, source{tag: "path", fn: fn})
	return b
}

// FromQuery adds a query parameter source using the request URL query.
func (b *Binder) FromQuery(r *http.Request) *Binder {
	b.sources = append(b.sources, source{tag: "query", fn: func(key string) string {
		return r.URL.Query().Get(key)
	}})
	return b
}

// FromHeader adds an HTTP header source.
func (b *Binder) FromHeader(r *http.Request) *Binder {
	b.sources = append(b.sources, source{tag: "header", fn: func(key string) string {
		return r.Header.Get(key)
	}})
	return b
}

// FromFunc adds a custom source with the given tag name.
func (b *Binder) FromFunc(tag string, fn SourceFunc) *Binder {
	b.sources = append(b.sources, source{tag: tag, fn: fn})
	return b
}

// Bind populates struct fields from all registered sources, then applies defaults.
func (b *Binder) Bind(target any) error {
	val := reflect.ValueOf(target)
	if val.Kind() != reflect.Ptr || val.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("bind: target must be a pointer to a struct")
	}

	elem := val.Elem()
	typ := elem.Type()
	values := make(map[int]string)

	for _, src := range b.sources {
		for i := 0; i < typ.NumField(); i++ {
			if _, ok := values[i]; ok {
				continue
			}
			fieldType := typ.Field(i)
			tagVal, ok := fieldType.Tag.Lookup(src.tag)
			if !ok || tagVal == "" {
				continue
			}
			if v := src.fn(tagVal); v != "" {
				values[i] = v
			}
		}
	}

	for i := 0; i < typ.NumField(); i++ {
		if _, ok := values[i]; ok {
			continue
		}
		fieldType := typ.Field(i)
		if def, ok := fieldType.Tag.Lookup("default"); ok && def != "" {
			values[i] = def
		}
	}

	for idx, v := range values {
		field := elem.Field(idx)
		if !field.CanSet() {
			continue
		}
		if err := setField(field, v); err != nil {
			typeField := typ.Field(idx)
			return fmt.Errorf("bind: field %s: %w", typeField.Name, err)
		}
	}
	return nil
}

// BindJSON unmarshals data into the struct field tagged with body:"json".
func (b *Binder) BindJSON(target any, data []byte) error {
	val := reflect.ValueOf(target)
	if val.Kind() != reflect.Ptr || val.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("bind: target must be a pointer to a struct")
	}

	elem := val.Elem()
	typ := elem.Type()

	for i := 0; i < elem.NumField(); i++ {
		field := elem.Field(i)
		fieldType := typ.Field(i)
		if tag, ok := fieldType.Tag.Lookup("body"); ok && tag == "json" && field.CanSet() {
			return json.Unmarshal(data, field.Addr().Interface())
		}
	}
	return nil
}

func setField(field reflect.Value, valStr string) error {
	switch field.Kind() {
	case reflect.String:
		field.SetString(valStr)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		val, err := strconv.ParseInt(valStr, 10, 64)
		if err != nil {
			return err
		}
		field.SetInt(val)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		val, err := strconv.ParseUint(valStr, 10, 64)
		if err != nil {
			return err
		}
		field.SetUint(val)
	case reflect.Bool:
		val, err := strconv.ParseBool(valStr)
		if err != nil {
			return err
		}
		field.SetBool(val)
	case reflect.Float32, reflect.Float64:
		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			return err
		}
		field.SetFloat(val)
	default:
		return fmt.Errorf("unsupported type %s", field.Kind())
	}
	return nil
}

// ioReadAll reads the request body up to 1MB.
func ioReadAll(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	defer r.Body.Close()
	return io.ReadAll(io.LimitReader(r.Body, 1<<20))
}