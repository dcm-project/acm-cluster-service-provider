package handler

import (
	"errors"
	"net/http"

	v1alpha1 "github.com/dcm-project/acm-cluster-service-provider/api/v1alpha1"
	"github.com/dcm-project/acm-cluster-service-provider/internal/service"
)

// DetailInternalError is the detail string for INTERNAL error responses.
const DetailInternalError = "an internal error occurred"

// ProblemFields holds RFC 9457 problem detail fields mapped from a domain error.
type ProblemFields struct {
	Type   v1alpha1.ErrorType
	Status int
	Title  string
	Detail string
}

// MapDomainError maps a domain error to RFC 9457 problem detail fields.
func MapDomainError(err error) ProblemFields {
	var domainErr *service.DomainError
	if !errors.As(err, &domainErr) {
		m := mapErrorType(v1alpha1.ErrorTypeINTERNAL)
		return ProblemFields{
			Type:   v1alpha1.ErrorTypeINTERNAL,
			Status: m.Status,
			Title:  m.Title,
			Detail: DetailInternalError,
		}
	}

	m := mapErrorType(domainErr.Type)

	detail := domainErr.Message
	if domainErr.Detail != "" {
		detail = domainErr.Detail
	}
	if domainErr.Type == v1alpha1.ErrorTypeINTERNAL {
		detail = DetailInternalError
	}

	return ProblemFields{
		Type:   domainErr.Type,
		Status: m.Status,
		Title:  m.Title,
		Detail: detail,
	}
}

// problemMapping combines HTTP status code and RFC 9457 title for a problem type.
// Titles follow the humanized-slug convention aligned with K8s Container SP.
type problemMapping struct {
	Status int
	Title  string
}

func mapErrorType(t v1alpha1.ErrorType) problemMapping {
	switch t {
	case v1alpha1.ErrorTypeINVALIDARGUMENT:
		return problemMapping{http.StatusBadRequest, "Invalid argument"}
	case v1alpha1.ErrorTypeNOTFOUND:
		return problemMapping{http.StatusNotFound, "Not found"}
	case v1alpha1.ErrorTypeALREADYEXISTS:
		return problemMapping{http.StatusConflict, "Already exists"}
	case v1alpha1.ErrorTypeUNPROCESSABLEENTITY:
		return problemMapping{http.StatusUnprocessableEntity, "Unprocessable entity"}
	case v1alpha1.ErrorTypeINTERNAL:
		return problemMapping{http.StatusInternalServerError, "Internal Server Error"}
	case v1alpha1.ErrorTypeUNAVAILABLE:
		return problemMapping{http.StatusServiceUnavailable, "Service unavailable"}
	case v1alpha1.ErrorTypePERMISSIONDENIED:
		return problemMapping{http.StatusForbidden, "Permission denied"}
	case v1alpha1.ErrorTypeUNAUTHENTICATED:
		return problemMapping{http.StatusUnauthorized, "Unauthenticated"}
	default:
		return problemMapping{http.StatusInternalServerError, "Internal Server Error"}
	}
}
