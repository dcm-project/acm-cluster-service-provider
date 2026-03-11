package apiserver

import "net/http"

// WrapHandler wraps the server's HTTP handler. This method is only
// available in test binaries (via export_test.go).
func (s *Server) WrapHandler(wrap func(http.Handler) http.Handler) {
	s.srv.Handler = wrap(s.srv.Handler)
}

// HTTPServer returns the underlying *http.Server for timeout assertions.
func (s *Server) HTTPServer() *http.Server {
	return s.srv
}

// ScrubValidationError is exported for unit testing via export_test.go.
var ScrubValidationError = scrubValidationError
