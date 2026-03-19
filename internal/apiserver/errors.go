package apiserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	v1alpha1 "github.com/dcm-project/acm-cluster-service-provider/api/v1alpha1"
	oapigen "github.com/dcm-project/acm-cluster-service-provider/internal/api/server"
	"github.com/dcm-project/acm-cluster-service-provider/internal/util"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
)

const (
	contentTypeProblemJSON = "application/problem+json"
)

// writeRFC7807 writes an RFC 7807 application/problem+json response.
func writeRFC7807(w http.ResponseWriter, logger *slog.Logger, statusCode int, errType v1alpha1.ErrorType, title, detail string) {
	status := int32(statusCode)
	resp := v1alpha1.Error{
		Type:   errType,
		Title:  title,
		Status: util.Ptr(status),
		Detail: util.Ptr(detail),
	}
	w.Header().Set("Content-Type", contentTypeProblemJSON)
	w.WriteHeader(statusCode)
	if encErr := json.NewEncoder(w).Encode(resp); encErr != nil {
		logger.Error("failed to encode error response", "error", encErr)
	}
}

// newBadRequestHandler returns a handler that writes a 400 Bad Request
// response with an RFC 7807 application/problem+json body. It is used
// by the parameter binding layer (generated chi wrapper) and OpenAPI
// validation middleware.
func newBadRequestHandler(logger *slog.Logger) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, _ *http.Request, err error) {
		writeRFC7807(w, logger, http.StatusBadRequest, v1alpha1.ErrorTypeINVALIDARGUMENT, "Bad Request", scrubValidationError(err))
	}
}

// scrubValidationError extracts a human-readable constraint message from
// kin-openapi validation errors, stripping raw schema JSON and value dumps.
// For unrecognised error types it returns a generic message to avoid leaking
// internal details to clients.
func scrubValidationError(err error) string {
	const genericMsg = "invalid request"

	var reqErr *openapi3filter.RequestError
	if errors.As(err, &reqErr) {
		var prefix string
		if p := reqErr.Parameter; p != nil {
			prefix = fmt.Sprintf("parameter %q in %s", p.Name, p.In)
		} else if reqErr.RequestBody != nil {
			prefix = "request body"
		}

		var schemaErr *openapi3.SchemaError
		if errors.As(reqErr.Err, &schemaErr) && schemaErr.Reason != "" {
			if prefix != "" {
				return prefix + ": " + schemaErr.Reason
			}
			return schemaErr.Reason
		}

		if reqErr.Reason != "" {
			if prefix != "" {
				return prefix + ": " + reqErr.Reason
			}
			return reqErr.Reason
		}

		return genericMsg
	}

	var paramErr *oapigen.InvalidParamFormatError
	if errors.As(err, &paramErr) {
		return fmt.Sprintf("invalid format for parameter %q", paramErr.ParamName)
	}

	var requiredErr *oapigen.RequiredParamError
	if errors.As(err, &requiredErr) {
		return fmt.Sprintf("missing required parameter %q", requiredErr.ParamName)
	}

	var headerErr *oapigen.RequiredHeaderError
	if errors.As(err, &headerErr) {
		return fmt.Sprintf("missing required header %q", headerErr.ParamName)
	}

	var cookieErr *oapigen.UnescapedCookieParamError
	if errors.As(err, &cookieErr) {
		return fmt.Sprintf("invalid cookie parameter %q", cookieErr.ParamName)
	}

	var unmarshalErr *oapigen.UnmarshalingParamError
	if errors.As(err, &unmarshalErr) {
		return fmt.Sprintf("invalid value for parameter %q", unmarshalErr.ParamName)
	}

	var tooManyErr *oapigen.TooManyValuesForParamError
	if errors.As(err, &tooManyErr) {
		return fmt.Sprintf("too many values for parameter %q", tooManyErr.ParamName)
	}

	return genericMsg
}

// responseWriter wraps an http.ResponseWriter to track the status code and
// whether headers have already been sent. It is used by both the recovery
// middleware (to avoid double-writing headers) and the logging middleware
// (to capture the status code).
type responseWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (w *responseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriter) Write(b []byte) (int, error) {
	w.wroteHeader = true
	return w.ResponseWriter.Write(b)
}

func (w *responseWriter) Flush() {
	w.wroteHeader = true
	if fl, ok := w.ResponseWriter.(http.Flusher); ok {
		fl.Flush()
	}
}

// Unwrap returns the underlying ResponseWriter, enabling Go 1.20+
// http.ResponseController to discover optional interfaces.
func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
