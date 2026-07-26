package openapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
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
	Name     string `json:"name"`
	In       string `json:"in"`
	Required bool   `json:"required"`
	Schema   Schema `json:"schema"`
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

	for index, h := range handlers {
		val := reflect.ValueOf(h)
		if !val.IsValid() {
			return nil, fmt.Errorf("openapi: handler %d is nil", index)
		}
		for val.Kind() == reflect.Ptr {
			if val.IsNil() {
				return nil, fmt.Errorf("openapi: handler %d is a nil pointer", index)
			}
			val = val.Elem()
		}
		if val.Kind() != reflect.Struct {
			return nil, fmt.Errorf("openapi: handler %d must be a struct or pointer to struct", index)
		}
		typ := val.Type()

		var method, path string
		extractTags(typ, &method, &path, make(map[reflect.Type]struct{}))

		if method == "" || path == "" {
			return nil, fmt.Errorf("openapi: handler %d must declare method and path", index)
		}
		if !validHTTPMethod(method) {
			return nil, fmt.Errorf("openapi: handler %d has unsupported HTTP method %q", index, method)
		}
		openAPIPath, pathParameters, err := normalizePath(path)
		if err != nil {
			return nil, fmt.Errorf("openapi: handler %d: %w", index, err)
		}

		op := Operation{
			Responses: map[string]Response{"200": {Description: "OK"}},
		}

		discoverParameters(typ, &op, make(map[reflect.Type]struct{}))

		if mp, ok := h.(MetaProvider); ok {
			m := mp.OpenAPIMeta()
			if s, ok := m["summary"].(string); ok {
				op.Summary = s
			}
			if d, ok := m["description"].(string); ok {
				op.Description = d
			}
			if paramsValue, exists := m["parameters"]; exists {
				params, ok := paramsValue.([]map[string]any)
				if !ok {
					return nil, fmt.Errorf("openapi: handler %d metadata parameters must be []map[string]any", index)
				}
				for _, pm := range params {
					name, nameOK := pm["name"].(string)
					location, locationOK := pm["in"].(string)
					if !nameOK || name == "" || !locationOK || location == "" {
						return nil, fmt.Errorf("openapi: handler %d metadata parameter requires string name and in", index)
					}
					p := Parameter{Name: name, In: location}
					if req, ok := pm["required"].(bool); ok {
						p.Required = req
					}
					if schemaValue, exists := pm["schema"]; exists {
						sch, ok := schemaValue.(map[string]any)
						if !ok {
							return nil, fmt.Errorf("openapi: handler %d metadata parameter schema must be map[string]any", index)
						}
						p.Schema.Type, _ = sch["type"].(string)
						if minimum, exists := sch["minimum"]; exists {
							min, ok := numericValue(minimum)
							if !ok {
								return nil, fmt.Errorf("openapi: handler %d metadata minimum must be numeric", index)
							}
							p.Schema.Minimum = &min
						}
					}
					op.Parameters = append(op.Parameters, p)
				}
			}
			if responsesValue, exists := m["responses"]; exists {
				resp, ok := responsesValue.(map[int]any)
				if !ok {
					return nil, fmt.Errorf("openapi: handler %d metadata responses must be map[int]any", index)
				}
				op.Responses = map[string]Response{}
				for code, desc := range resp {
					description, ok := desc.(string)
					if !ok {
						return nil, fmt.Errorf("openapi: handler %d response %d description must be a string", index, code)
					}
					op.Responses[codeToStr(code)] = Response{Description: description}
				}
			}
		}

		if err := validateParameters(op.Parameters, pathParameters); err != nil {
			return nil, fmt.Errorf("openapi: handler %d: %w", index, err)
		}

		pathItem, ok := doc.Paths[openAPIPath]
		if !ok {
			pathItem = make(PathItem)
		}
		if _, exists := pathItem[method]; exists {
			return nil, fmt.Errorf("openapi: duplicate operation %s %s", strings.ToUpper(method), openAPIPath)
		}
		pathItem[method] = op
		doc.Paths[openAPIPath] = pathItem
	}

	return json.MarshalIndent(doc, "", "  ")
}

func validHTTPMethod(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodConnect,
		http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
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
	return strconv.Itoa(code)
}

func extractTags(typ reflect.Type, method, path *string, active map[reflect.Type]struct{}) {
	typ = indirectType(typ)
	if typ == nil || typ.Kind() != reflect.Struct {
		return
	}
	if _, exists := active[typ]; exists {
		return
	}
	active[typ] = struct{}{}
	defer delete(active, typ)

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Anonymous {
			extractTags(field.Type, method, path, active)
		}
		if m := field.Tag.Get("method"); m != "" {
			*method = strings.ToLower(m)
			*path = field.Tag.Get("path")
		}
	}
}

func discoverParameters(typ reflect.Type, op *Operation, active map[reflect.Type]struct{}) {
	typ = indirectType(typ)
	if typ == nil || typ.Kind() != reflect.Struct {
		return
	}
	if _, exists := active[typ]; exists {
		return
	}
	active[typ] = struct{}{}
	defer delete(active, typ)

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Tag.Get("method") != "" {
			continue
		}
		if field.Anonymous {
			discoverParameters(field.Type, op, active)
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

func normalizePath(path string) (string, map[string]struct{}, error) {
	if !strings.HasPrefix(path, "/") {
		return "", nil, fmt.Errorf("path must start with /")
	}

	parameters := make(map[string]struct{})
	segments := strings.Split(path, "/")
	for index, segment := range segments {
		if !strings.ContainsAny(segment, "{}") {
			continue
		}
		if len(segment) < 3 || segment[0] != '{' || segment[len(segment)-1] != '}' {
			return "", nil, fmt.Errorf("malformed path segment %q", segment)
		}

		definition := segment[1 : len(segment)-1]
		if strings.HasPrefix(definition, "*") {
			definition = strings.TrimPrefix(definition, "*")
			if index != len(segments)-1 || strings.Contains(definition, ":") {
				return "", nil, fmt.Errorf("malformed catch-all path segment %q", segment)
			}
		} else if separator := strings.IndexByte(definition, ':'); separator >= 0 {
			definition = definition[:separator]
		}
		if !validParameterName(definition) {
			return "", nil, fmt.Errorf("invalid path parameter name %q", definition)
		}
		if _, exists := parameters[definition]; exists {
			return "", nil, fmt.Errorf("duplicate path parameter %q", definition)
		}
		parameters[definition] = struct{}{}
		segments[index] = "{" + definition + "}"
	}

	return strings.Join(segments, "/"), parameters, nil
}

func validParameterName(name string) bool {
	if name == "" {
		return false
	}
	for index, character := range name {
		if index == 0 {
			if character != '_' &&
				(character < 'A' || character > 'Z') &&
				(character < 'a' || character > 'z') {
				return false
			}
			continue
		}
		if character != '_' &&
			(character < 'A' || character > 'Z') &&
			(character < 'a' || character > 'z') &&
			(character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func validateParameters(parameters []Parameter, pathParameters map[string]struct{}) error {
	seen := make(map[string]struct{}, len(parameters))
	matchedPathParameters := make(map[string]struct{}, len(pathParameters))
	for _, parameter := range parameters {
		key := parameter.In + "\x00" + parameter.Name
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate %s parameter %q", parameter.In, parameter.Name)
		}
		seen[key] = struct{}{}

		if parameter.In != "path" {
			continue
		}
		if _, exists := pathParameters[parameter.Name]; !exists {
			return fmt.Errorf("path parameter %q is not present in the route", parameter.Name)
		}
		if !parameter.Required {
			return fmt.Errorf("path parameter %q must be required", parameter.Name)
		}
		matchedPathParameters[parameter.Name] = struct{}{}
	}
	for name := range pathParameters {
		if _, exists := matchedPathParameters[name]; !exists {
			return fmt.Errorf("route path parameter %q has no matching field", name)
		}
	}
	return nil
}

func indirectType(typ reflect.Type) reflect.Type {
	for typ != nil && typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	return typ
}

func numericValue(value any) (float64, bool) {
	switch number := value.(type) {
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case float64:
		return number, true
	default:
		return 0, false
	}
}
