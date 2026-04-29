package srv

import (
	"github.com/mirkobrombin/go-foundation/pkg/validation"
)

// Validate runs validation rules on the target object and returns the collected
// validation errors. Returns nil if there are no errors.
func (c *Context) Validate(target any) validation.Errors {
	return validation.New().Validate(target)
}

// BindAndValidate decodes the request body into target and then runs validation.
// Returns a validation.Errors slice if decoding or validation fails.
func (c *Context) BindAndValidate(target any) validation.Errors {
	if err := c.Bind(target); err != nil {
		return validation.Errors{{Field: "", Message: err.Error()}}
	}
	return c.Validate(target)
}
