package handler

import (
	"errors"

	v1alpha1 "github.com/dcm-project/acm-cluster-service-provider/api/v1alpha1"
	"github.com/dcm-project/acm-cluster-service-provider/internal/service"
)

// MapDomainError maps a domain error to RFC 7807 error fields.
// Returns (errorType, httpStatus, title, detail).
func MapDomainError(err error) (v1alpha1.ErrorType, int, string, string) {
	var domainErr *service.DomainError
	if !errors.As(err, &domainErr) {
		return v1alpha1.INTERNAL, 500, "Internal Server Error", "an internal error occurred"
	}

	status := mapErrorTypeToStatus(domainErr.Type)
	title := mapErrorTypeToTitle(domainErr.Type)

	detail := domainErr.Message
	if domainErr.Detail != "" {
		detail = domainErr.Detail
	}

	if domainErr.Type == v1alpha1.INTERNAL {
		detail = "an internal error occurred"
	}

	return domainErr.Type, status, title, detail
}

func mapErrorTypeToStatus(t v1alpha1.ErrorType) int {
	switch t {
	case v1alpha1.INVALIDARGUMENT:
		return 400
	case v1alpha1.NOTFOUND:
		return 404
	case v1alpha1.ALREADYEXISTS:
		return 409
	case v1alpha1.UNPROCESSABLEENTITY:
		return 422
	case v1alpha1.INTERNAL:
		return 500
	case v1alpha1.UNAVAILABLE:
		return 503
	default:
		return 500
	}
}

func mapErrorTypeToTitle(t v1alpha1.ErrorType) string {
	switch t {
	case v1alpha1.INVALIDARGUMENT:
		return "Bad Request"
	case v1alpha1.NOTFOUND:
		return "Not Found"
	case v1alpha1.ALREADYEXISTS:
		return "Conflict"
	case v1alpha1.UNPROCESSABLEENTITY:
		return "Unprocessable Entity"
	case v1alpha1.INTERNAL:
		return "Internal Server Error"
	case v1alpha1.UNAVAILABLE:
		return "Service Unavailable"
	default:
		return "Internal Server Error"
	}
}
