// Package handler implements the strict server interface by delegating to domain services.
package handler

import (
	"context"

	oapigen "github.com/dcm-project/acm-cluster-service-provider/internal/api/server"
	"github.com/dcm-project/acm-cluster-service-provider/internal/service"
	"github.com/dcm-project/acm-cluster-service-provider/internal/util"
	"github.com/google/uuid"
)

// Compile-time interface check.
var _ oapigen.StrictServerInterface = (*Handler)(nil)

// Handler implements StrictServerInterface by delegating to domain services.
type Handler struct {
	clusterService service.ClusterService
	healthChecker  service.HealthChecker
}

// New creates a Handler with the given dependencies.
func New(clusterService service.ClusterService, healthChecker service.HealthChecker) *Handler {
	return &Handler{
		clusterService: clusterService,
		healthChecker:  healthChecker,
	}
}

// GetHealth delegates to the HealthChecker and returns the result.
func (h *Handler) GetHealth(ctx context.Context, _ oapigen.GetHealthRequestObject) (oapigen.GetHealthResponseObject, error) {
	result := h.healthChecker.Check(ctx)
	return oapigen.GetHealth200JSONResponse(result), nil
}

// CreateCluster validates the request, delegates to the cluster service, and returns the result.
func (h *Handler) CreateCluster(ctx context.Context, req oapigen.CreateClusterRequestObject) (oapigen.CreateClusterResponseObject, error) {
	// Resolve ID: client-specified or server-generated.
	id := uuid.New().String()
	if req.Params.Id != nil && *req.Params.Id != "" {
		id = *req.Params.Id
		if err := validateClientID(id); err != nil {
			return createError400(err.Error()), nil
		}
	}

	// Validate request body.
	if err := validateCreateRequest(req.Body); err != nil {
		return createError400(err.Error()), nil
	}

	// Strip read-only fields from body.
	req.Body.Id = nil
	req.Body.Status = nil
	req.Body.ApiEndpoint = nil
	req.Body.Kubeconfig = nil
	req.Body.CreateTime = nil
	req.Body.UpdateTime = nil
	req.Body.Path = nil
	req.Body.StatusMessage = nil
	req.Body.ConsoleUri = nil

	// Convert to service-layer type.
	svcCluster, err := toServiceCluster(*req.Body)
	if err != nil {
		return createError500(), nil //nolint:nilerr // error translated to HTTP 500 response
	}

	// Delegate to service.
	result, err := h.clusterService.Create(ctx, id, svcCluster)
	if err != nil {
		return mapCreateError(err), nil
	}

	// Convert result back to API type.
	apiCluster, err := toAPICluster(result)
	if err != nil {
		return createError500(), nil //nolint:nilerr // error translated to HTTP 500 response
	}
	return oapigen.CreateCluster201JSONResponse(apiCluster), nil
}

// GetCluster delegates to the cluster service and returns the result.
func (h *Handler) GetCluster(ctx context.Context, req oapigen.GetClusterRequestObject) (oapigen.GetClusterResponseObject, error) {
	result, err := h.clusterService.Get(ctx, req.ClusterId)
	if err != nil {
		return mapGetError(err), nil
	}

	apiCluster, err := toAPICluster(result)
	if err != nil {
		return getError500(), nil //nolint:nilerr // error translated to HTTP 500 response
	}
	return oapigen.GetCluster200JSONResponse(apiCluster), nil
}

// ListClusters validates pagination params, delegates to the cluster service, and returns the result.
func (h *Handler) ListClusters(ctx context.Context, req oapigen.ListClustersRequestObject) (oapigen.ListClustersResponseObject, error) {
	// Resolve max_page_size: default 50.
	pageSize := int32(50)
	if req.Params.MaxPageSize != nil {
		pageSize = *req.Params.MaxPageSize
		if err := validateMaxPageSize(pageSize); err != nil {
			return listError400(err.Error()), nil
		}
	}

	// Resolve page_token: nil or empty treated as absent.
	pageToken := ""
	if req.Params.PageToken != nil {
		pageToken = *req.Params.PageToken
	}

	// Delegate to service.
	result, err := h.clusterService.List(ctx, int(pageSize), pageToken)
	if err != nil {
		return mapListError(err), nil
	}

	// Convert result to API type.
	apiList, err := toAPIClusterList(result)
	if err != nil {
		return listError500(), nil //nolint:nilerr // error translated to HTTP 500 response
	}
	return oapigen.ListClusters200JSONResponse(apiList), nil
}

// DeleteCluster delegates to the cluster service and returns 204 on success.
func (h *Handler) DeleteCluster(ctx context.Context, req oapigen.DeleteClusterRequestObject) (oapigen.DeleteClusterResponseObject, error) {
	err := h.clusterService.Delete(ctx, req.ClusterId)
	if err != nil {
		return mapDeleteError(err), nil
	}
	return oapigen.DeleteCluster204Response{}, nil
}

// Per-operation error mappers.

func buildErrorResponse(errType oapigen.ErrorType, status int32, title, detail string) oapigen.Error {
	return oapigen.Error{
		Type:   errType,
		Title:  title,
		Status: util.Ptr(status),
		Detail: util.Ptr(detail),
	}
}

func createError400(detail string) oapigen.CreateCluster400ApplicationProblemPlusJSONResponse {
	return oapigen.CreateCluster400ApplicationProblemPlusJSONResponse(
		buildErrorResponse(oapigen.INVALIDARGUMENT, 400, "Bad Request", detail),
	)
}

func createError500() oapigen.CreateCluster500ApplicationProblemPlusJSONResponse {
	return oapigen.CreateCluster500ApplicationProblemPlusJSONResponse(
		buildErrorResponse(oapigen.INTERNAL, 500, "Internal Server Error", "an internal error occurred"),
	)
}

func mapCreateError(err error) oapigen.CreateClusterResponseObject {
	errType, status, title, detail := MapDomainError(err)
	errObj := buildErrorResponse(oapigen.ErrorType(errType), int32(status), title, detail)
	switch status {
	case 400:
		return oapigen.CreateCluster400ApplicationProblemPlusJSONResponse(errObj)
	case 409:
		return oapigen.CreateCluster409ApplicationProblemPlusJSONResponse(errObj)
	case 422:
		return oapigen.CreateCluster422ApplicationProblemPlusJSONResponse(errObj)
	default:
		return oapigen.CreateCluster500ApplicationProblemPlusJSONResponse(errObj)
	}
}

func getError500() oapigen.GetCluster500ApplicationProblemPlusJSONResponse {
	return oapigen.GetCluster500ApplicationProblemPlusJSONResponse(
		buildErrorResponse(oapigen.INTERNAL, 500, "Internal Server Error", "an internal error occurred"),
	)
}

func mapGetError(err error) oapigen.GetClusterResponseObject {
	errType, status, title, detail := MapDomainError(err)
	errObj := buildErrorResponse(oapigen.ErrorType(errType), int32(status), title, detail)
	switch status {
	case 404:
		return oapigen.GetCluster404ApplicationProblemPlusJSONResponse(errObj)
	default:
		return oapigen.GetCluster500ApplicationProblemPlusJSONResponse(errObj)
	}
}

func listError400(detail string) oapigen.ListClusters400ApplicationProblemPlusJSONResponse {
	return oapigen.ListClusters400ApplicationProblemPlusJSONResponse(
		buildErrorResponse(oapigen.INVALIDARGUMENT, 400, "Bad Request", detail),
	)
}

func listError500() oapigen.ListClusters500ApplicationProblemPlusJSONResponse {
	return oapigen.ListClusters500ApplicationProblemPlusJSONResponse(
		buildErrorResponse(oapigen.INTERNAL, 500, "Internal Server Error", "an internal error occurred"),
	)
}

func mapListError(err error) oapigen.ListClustersResponseObject {
	errType, status, title, detail := MapDomainError(err)
	errObj := buildErrorResponse(oapigen.ErrorType(errType), int32(status), title, detail)
	switch status {
	case 400:
		return oapigen.ListClusters400ApplicationProblemPlusJSONResponse(errObj)
	default:
		return oapigen.ListClusters500ApplicationProblemPlusJSONResponse(errObj)
	}
}

func mapDeleteError(err error) oapigen.DeleteClusterResponseObject {
	errType, status, title, detail := MapDomainError(err)
	errObj := buildErrorResponse(oapigen.ErrorType(errType), int32(status), title, detail)
	switch status {
	case 404:
		return oapigen.DeleteCluster404ApplicationProblemPlusJSONResponse(errObj)
	default:
		return oapigen.DeleteCluster500ApplicationProblemPlusJSONResponse(errObj)
	}
}
