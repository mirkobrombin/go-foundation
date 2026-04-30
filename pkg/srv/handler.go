package srv

import (
	"context"
	"io"
	"net/http"
	"reflect"

	"github.com/mirkobrombin/go-foundation/pkg/bind"
	"github.com/mirkobrombin/go-foundation/pkg/di"
)

// Handler is the interface for declarative struct-tagged endpoints.
type Handler interface {
	Handle(ctx context.Context) (any, error)
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
func (s *Server) RegisterHandler(prototype Handler, container *di.Container) {
	meta := &handlerMeta{
		prototype: reflect.TypeOf(prototype).Elem(),
		container: container,
	}

	typ := meta.prototype
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
		panic("RegisterHandler: struct must have method and path struct tags")
	}

	meta.method = method
	meta.path = path

	handler := s.buildHandler(meta)
	s.tree.insert(method, path, handler)
}

type handlerMeta struct {
	prototype reflect.Type
	method    string
	path      string
	container *di.Container
}

func (s *Server) buildHandler(meta *handlerMeta) HandlerFunc {
	return func(ctx *Context) error {
		newVal := reflect.New(meta.prototype)

		if meta.container != nil {
			meta.container.Inject(newVal.Interface())
		}

		b := bind.New()
		if len(ctx.Params) > 0 {
			b.FromPath(func(key string) string {
				return ctx.Params[key]
			})
		}
		b.FromQuery(ctx.Request)
		b.FromHeader(ctx.Request)
		b.Bind(newVal.Interface())

		if ctx.Request.Body != nil {
			if ct := ctx.Request.Header.Get("Content-Type"); ct == "application/json" {
				body, err := io.ReadAll(ctx.Request.Body)
				if err == nil && len(body) > 0 {
					_ = bind.New().BindJSON(newVal.Interface(), body)
				}
			}
		}

		result, err := newVal.Interface().(Handler).Handle(ctx.Request.Context())
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