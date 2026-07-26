package web

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"reflect"
	"strings"

	"github.com/mirkobrombin/go-foundation/v2/app/di"
	"github.com/mirkobrombin/go-foundation/v2/core/bind"
)

// Handler is the interface for declarative struct-tagged endpoints.
type Handler interface {
	Handle(ctx context.Context) (any, error)
}

// HandlerDefinition describes a handler without runtime metadata discovery.
type HandlerDefinition struct {
	Method string
	Path   string
	New    func() Handler
}

// RegisterHandler registers a struct that implements Handler as an endpoint.
// The struct must have method and path tags:
//
//	type MyEndpoint struct {
//	    Pattern `method:"GET" path:"/api/v1/ping"`
//	    Times   int `query:"times" default:"1"`
//	}
//
// The container is used for dependency injection and bind for field population.
func (s *Server) RegisterHandler(prototype Handler, container *di.Container) error {
	definition, err := DefinitionFromHandler(prototype)
	if err != nil {
		return err
	}
	return s.RegisterDefinition(definition, container)
}

// DefinitionFromHandler describes a reflection-based prototype for batch validation.
func DefinitionFromHandler(prototype Handler) (HandlerDefinition, error) {
	value := reflect.ValueOf(prototype)
	if !value.IsValid() || value.Kind() != reflect.Ptr || value.IsNil() || value.Elem().Kind() != reflect.Struct {
		return HandlerDefinition{}, fmt.Errorf("web: handler prototype must be a non-nil pointer to a struct")
	}
	typ := value.Elem().Type()
	var method, path string
	for i := 0; i < typ.NumField(); i++ {
		sf := typ.Field(i)
		if m := sf.Tag.Get("method"); m != "" {
			method = m
		}
		if p := sf.Tag.Get("path"); p != "" {
			path = p
		}
	}

	if method == "" || path == "" {
		return HandlerDefinition{}, fmt.Errorf("web: handler %T must declare method and path tags", prototype)
	}

	return HandlerDefinition{
		Method: method,
		Path:   path,
		New: func() Handler {
			newVal := reflect.New(typ)
			newVal.Elem().Set(value.Elem())
			return newVal.Interface().(Handler)
		},
	}, nil
}

// RegisterDefinition registers a statically described handler.
func (s *Server) RegisterDefinition(def HandlerDefinition, container *di.Container) error {
	return s.RegisterDefinitions(container, def)
}

// ValidateDefinitions checks static handlers without changing the server.
func (s *Server) ValidateDefinitions(container *di.Container, defs ...HandlerDefinition) error {
	prepared, err := s.prepareDefinitions(container, defs)
	if err != nil {
		return err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return validatePreparedDefinitions(s.routes, prepared)
}

// RegisterDefinitions validates and registers static handlers as one operation.
func (s *Server) RegisterDefinitions(container *di.Container, defs ...HandlerDefinition) error {
	prepared, err := s.prepareDefinitions(container, defs)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validatePreparedDefinitions(s.routes, prepared); err != nil {
		return err
	}
	for _, item := range prepared {
		if err := s.tree.insert(item.meta.method, item.meta.path, item.handler); err != nil {
			return err
		}
		s.routes = append(s.routes, route{
			method:  item.meta.method,
			path:    item.meta.path,
			handler: item.handler,
		})
	}
	return nil
}

type preparedDefinition struct {
	meta    *handlerMeta
	handler HandlerFunc
}

func (s *Server) prepareDefinitions(container *di.Container, defs []HandlerDefinition) ([]preparedDefinition, error) {
	prepared := make([]preparedDefinition, 0, len(defs))
	for _, def := range defs {
		item, err := s.prepareDefinition(container, def)
		if err != nil {
			return nil, err
		}
		prepared = append(prepared, item)
	}
	return prepared, nil
}

func (s *Server) prepareDefinition(container *di.Container, def HandlerDefinition) (preparedDefinition, error) {
	if def.Method == "" || def.Path == "" {
		return preparedDefinition{}, fmt.Errorf("web: handler definition requires method and path")
	}
	if def.New == nil {
		return preparedDefinition{}, fmt.Errorf("web: handler definition requires a constructor")
	}
	prototype := def.New()
	if prototype == nil {
		return preparedDefinition{}, fmt.Errorf("web: handler constructor returned nil")
	}
	if container != nil {
		if err := container.ValidateTarget(prototype); err != nil {
			return preparedDefinition{}, fmt.Errorf("web: invalid handler %T: %w", prototype, err)
		}
	}

	meta := &handlerMeta{
		new:       def.New,
		name:      fmt.Sprintf("%T", prototype),
		method:    def.Method,
		path:      def.Path,
		container: container,
		jsonBody:  hasJSONBody(prototype),
	}

	handler := s.buildHandler(meta)
	return preparedDefinition{meta: meta, handler: handler}, nil
}

func validatePreparedDefinitions(routes []route, prepared []preparedDefinition) error {
	tree := newRadixTree()
	for _, registered := range routes {
		if err := tree.insert(registered.method, registered.path, registered.handler); err != nil {
			return err
		}
	}
	for _, item := range prepared {
		if err := tree.insert(item.meta.method, item.meta.path, item.handler); err != nil {
			return err
		}
	}
	return nil
}

type handlerMeta struct {
	new       func() Handler
	name      string
	method    string
	path      string
	container *di.Container
	jsonBody  bool
}

func (s *Server) buildHandler(meta *handlerMeta) HandlerFunc {
	return func(ctx *Context) error {
		handler := meta.new()

		if meta.container != nil {
			if err := meta.container.Inject(handler); err != nil {
				return fmt.Errorf("web: inject %s: %w", meta.name, err)
			}
		}

		b := bind.New()
		if len(ctx.Params) > 0 {
			b.FromPath(func(key string) string {
				return ctx.Params[key]
			})
		}
		b.FromQuery(ctx.Request)
		b.FromHeader(ctx.Request)
		if err := b.Bind(handler); err != nil {
			return Error(http.StatusBadRequest, fmt.Sprintf("web: invalid request input: %v", err))
		}

		if meta.jsonBody && ctx.Request.Body != nil && ctx.Request.ContentLength != 0 {
			if !isJSONContentType(ctx.Request.Header.Get("Content-Type")) {
				return Error(http.StatusUnsupportedMediaType, "web: JSON body requires application/json content type")
			}
			body, err := io.ReadAll(io.LimitReader(ctx.Request.Body, maxRequestBodySize+1))
			if err != nil {
				return fmt.Errorf("web: read request body: %w", err)
			}
			if len(body) > maxRequestBodySize {
				return Error(http.StatusRequestEntityTooLarge, "web: request body exceeds 1 MiB")
			}
			if len(body) > 0 {
				if err := bind.New().BindJSON(handler, body); err != nil {
					return Error(http.StatusBadRequest, fmt.Sprintf("web: invalid request body: %v", err))
				}
			}
		}

		result, err := handler.Handle(ctx.Request.Context())
		if err != nil {
			return err
		}
		if result == nil {
			ctx.Response.WriteHeader(http.StatusNoContent)
			return nil
		}
		return ctx.JSON(http.StatusOK, result)
	}
}

func hasJSONBody(handler Handler) bool {
	typ := reflect.TypeOf(handler)
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	for i := 0; i < typ.NumField(); i++ {
		if typ.Field(i).Tag.Get("body") == "json" {
			return true
		}
	}
	return false
}

func isJSONContentType(value string) bool {
	contentType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return false
	}
	return contentType == "application/json" || strings.HasSuffix(contentType, "+json")
}
