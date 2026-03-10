package apiserver

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	v1alpha1 "github.com/dcm-project/acm-cluster-service-provider/api/v1alpha1"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
)

// rfc7807RecoveryMiddleware catches panics and returns an RFC 7807
// application/problem+json response instead of a plain-text stack trace.
//
// Special cases:
//   - http.ErrAbortHandler is re-panicked so net/http aborts the connection.
//   - If the handler already called WriteHeader/Write, the middleware logs the
//     panic but does not attempt to write a response (headers already on the wire).
func rfc7807RecoveryMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			defer func() {
				if rec := recover(); rec != nil {
					if rec == http.ErrAbortHandler {
						panic(http.ErrAbortHandler)
					}

					logger.Error("panic recovered", "panic", rec, "stack", string(debug.Stack()))

					if rw.wroteHeader {
						logger.Warn("headers already sent, cannot write RFC 7807 response")
						return
					}

					writeRFC7807(w, logger, http.StatusInternalServerError, v1alpha1.INTERNAL, "Internal Server Error", "an unexpected error occurred")
				}
			}()
			next.ServeHTTP(rw, r)
		})
	}
}

// requestTimeoutMiddleware wraps the handler with a context deadline. If the
// request exceeds the configured timeout, the context is cancelled. The
// handler is responsible for checking ctx.Done() and returning early.
func requestTimeoutMiddleware(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// requestLoggingMiddleware logs each request with method, path, status code,
// and duration as structured log fields.
func requestLoggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(rw, r)
			logger.Info("request completed",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rw.statusCode,
				"duration", time.Since(start).String(),
			)
		})
	}
}

// openAPIValidationMiddleware validates incoming requests against the OpenAPI
// spec. Routes not found in the spec are passed through to the chi router.
func openAPIValidationMiddleware(specRouter routers.Router, badReq func(http.ResponseWriter, *http.Request, error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			route, pathParams, err := specRouter.FindRoute(r)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			input := &openapi3filter.RequestValidationInput{
				Request:    r,
				PathParams: pathParams,
				Route:      route,
			}

			if err := openapi3filter.ValidateRequest(r.Context(), input); err != nil {
				badReq(w, r, err)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
