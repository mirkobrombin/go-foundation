package openapi

import (
	"encoding/json"
	"reflect"
	"strings"
)

// Document represents an OpenAPI 3.0.3 document.
type Document struct {
	OpenAPI string              `json:"openapi"`
	Info    Info                `json:"info"`
	Paths   map[string]PathItem `json:"paths"`
}

// Info holds the API title and version.
type Info struct {
	Title   string `json:"title"`
	Version string `json:"version"`
}

// PathItem maps HTTP methods to operations for a path.
type PathItem map[string]Operation

// Operation describes a single API operation.
type Operation struct {
	Summary     string              `json:"summary,omitempty"`
	Description string              `json:"description,omitempty"`
	Parameters  []Parameter         `json:"parameters,omitempty"`
	Responses   map[string]Response `json:"responses"`
}

// Parameter describes an operation parameter.
type Parameter struct {
	Name     string  `json:"name"`
	In       string  `json:"in"`
	Required bool    `json:"required"`
	Schema   Schema  `json:"schema"`
}

// Schema describes a parameter schema.
type Schema struct {
	Type    string   `json:"type,omitempty"`
	Minimum *float64 `json:"minimum,omitempty"`
	Enum    []string `json:"enum,omitempty"`
}

// Response describes an operation response.
type Response struct {
	Description string `json:"description"`
}

// MetaProvider is an optional interface endpoints can implement for OpenAPI metadata.
type MetaProvider interface {
	OpenAPIMeta() map[string]any
}

// Build generates an OpenAPI 3.0.3 JSON document from struct-tagged handlers.
// Each handler must have method and path struct tags.
func Build(title, version string, handlers ...any) ([]byte, error) {
	doc := Document{
		OpenAPI: "3.0.3",
		Info: Info{
			Title:   title,
			Version: version,
		},
		Paths: map[string]PathItem{},
	}

	for _, h := range handlers {
		val := reflect.ValueOf(h)
		if val.Kind() == reflect.Ptr {
			val = val.Elem()
		}
		typ := val.Type()

		var method, path string
		extractTags(typ, &method, &path)

		if method == "" || path == "" {
			continue
		}

		op := Operation{
			Responses: map[string]Response{"200": {Description: "OK"}},
		}

		discoverParameters(typ, &op)

		if mp, ok := h.(MetaProvider); ok {
			m := mp.OpenAPIMeta()
			if s, ok := m["summary"].(string); ok {
				op.Summary = s
			}
			if d, ok := m["description"].(string); ok {
				op.Description = d
			}
			if params, ok := m["parameters"].([]map[string]any); ok {
				for _, pm := range params {
					p := Parameter{Name: pm["name"].(string), In: pm["in"].(string)}
					if req, ok := pm["required"].(bool); ok {
						p.Required = req
					}
					if sch, ok := pm["schema"].(map[string]any); ok {
						p.Schema.Type, _ = sch["type"].(string)
						if min, ok := sch["minimum"].(int); ok {
							f := float64(min)
							p.Schema.Minimum = &f
						}
					}
					op.Parameters = append(op.Parameters, p)
				}
			}
			if resp, ok := m["responses"].(map[int]any); ok {
				op.Responses = map[string]Response{}
				for code, desc := range resp {
					op.Responses[codeToStr(code)] = Response{Description: desc.(string)}
				}
			}
		}

		pathItem, ok := doc.Paths[path]
		if !ok {
			pathItem = make(PathItem)
		}
		pathItem[method] = op
		doc.Paths[path] = pathItem
	}

	return json.MarshalIndent(doc, "", "  ")
}

func goTypeToOpenAPI(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "integer"
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Bool:
		return "boolean"
	default:
		return "string"
	}
}

func codeToStr(code int) string {
	s := ""
	s += string(rune('0' + code/100))
	s += string(rune('0' + (code/10)%10))
	s += string(rune('0' + code%10))
	return s
}

func extractTags(typ reflect.Type, method, path *string) {
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Anonymous {
			extractTags(field.Type, method, path)
		}
		if m := field.Tag.Get("method"); m != "" {
			*method = strings.ToLower(m)
		}
		if p := field.Tag.Get("path"); p != "" {
			*path = p
		}
	}
}

func discoverParameters(typ reflect.Type, op *Operation) {
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Anonymous {
			discoverParameters(field.Type, op)
			continue
		}
		if q := field.Tag.Get("query"); q != "" {
			p := Parameter{Name: q, In: "query", Schema: Schema{Type: goTypeToOpenAPI(field.Type)}}
			op.Parameters = append(op.Parameters, p)
		}
		if p := field.Tag.Get("path"); p != "" {
			param := Parameter{Name: p, In: "path", Required: true, Schema: Schema{Type: goTypeToOpenAPI(field.Type)}}
			op.Parameters = append(op.Parameters, param)
		}
		if h := field.Tag.Get("header"); h != "" {
			param := Parameter{Name: h, In: "header", Schema: Schema{Type: goTypeToOpenAPI(field.Type)}}
			op.Parameters = append(op.Parameters, param)
		}
	}
}