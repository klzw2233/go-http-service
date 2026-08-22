package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"

	"go-http-service/internal/model"
)

var validatorOnce sync.Once

// configureValidator makes validation errors report the JSON field name
// ("message") rather than the Go struct field name ("Message"). Without
// it, validator renders errors as
//
//	Key: 'EchoRequest.Message' Error:Field validation for 'Message' ...
//
// which puts an internal type name on the wire. Safe to call repeatedly.
//
// This stays a package-level function rather than an API method: it
// mutates gin's process-wide validator, so once-per-process is the
// correct scope even when several API values exist in tests.
func configureValidator() {
	validatorOnce.Do(func() {
		v, ok := binding.Validator.Engine().(*validator.Validate)
		if !ok {
			return
		}
		v.RegisterTagNameFunc(func(fld reflect.StructField) string {
			name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
			if name == "-" {
				return ""
			}
			return name
		})
	})
}

// respondError writes an ErrorResponse with no per-field detail.
func (a *API) respondError(c *gin.Context, status int, code model.ErrorCode, message string) {
	c.AbortWithStatusJSON(status, model.NewErrorResponse(code, message))
}

// respondBindError translates a request-binding failure into the
// project's error shape.
//
// The raw error is logged but never returned: validator and
// encoding/json both embed Go type names in their messages, and their
// wording changes across dependency versions, so callers cannot rely on
// it. Logging happens through logFor so the record carries the same
// request ID as the access log line.
func (a *API) respondBindError(c *gin.Context, err error) {
	a.logFor(c).Warn("request binding failed",
		"method", c.Request.Method,
		"path", c.Request.URL.Path,
		"error", err)

	// Body exceeded the limit set by the limitBodySize middleware.
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		a.respondError(c, http.StatusRequestEntityTooLarge, model.ErrCodePayloadTooLarge,
			fmt.Sprintf("request body must not exceed %d bytes", tooLarge.Limit))
		return
	}

	// One or more fields failed a binding tag such as required or max.
	var invalid validator.ValidationErrors
	if errors.As(err, &invalid) {
		fields := make([]model.FieldError, 0, len(invalid))
		for _, fe := range invalid {
			fields = append(fields, model.FieldError{
				Field:  fe.Field(),
				Reason: validationReason(fe),
			})
		}
		c.AbortWithStatusJSON(http.StatusBadRequest, model.ErrorResponse{
			Code:    model.ErrCodeValidationFailed,
			Message: "one or more fields failed validation",
			Fields:  fields,
		})
		return
	}

	// A field held the wrong JSON type. UnmarshalTypeError.Field is the
	// JSON path, so it is safe to echo back.
	var wrongType *json.UnmarshalTypeError
	if errors.As(err, &wrongType) {
		c.AbortWithStatusJSON(http.StatusBadRequest, model.ErrorResponse{
			Code:    model.ErrCodeValidationFailed,
			Message: "one or more fields have the wrong type",
			Fields: []model.FieldError{{
				Field:  wrongType.Field,
				Reason: fmt.Sprintf("expected a %s, got a %s", wrongType.Type.Kind(), wrongType.Value),
			}},
		})
		return
	}

	// Malformed or truncated JSON.
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		a.respondError(c, http.StatusBadRequest, model.ErrCodeInvalidJSON,
			"request body is not valid JSON")
		return
	}

	// Unrecognised failure. Stay generic rather than risk leaking internals.
	a.respondError(c, http.StatusBadRequest, model.ErrCodeInvalidJSON,
		"request body could not be processed")
}

// validationReason renders one failed constraint in plain language,
// without naming the validator library or the Go type.
func validationReason(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "is required"
	case "min":
		return fmt.Sprintf("must be at least %s characters", fe.Param())
	case "max":
		return fmt.Sprintf("must be at most %s characters", fe.Param())
	default:
		return fmt.Sprintf("failed the %q constraint", fe.Tag())
	}
}
