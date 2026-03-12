package handler

import (
	"context"

	oapigen "github.com/dcm-project/acm-cluster-service-provider/internal/api/server"
	"github.com/dcm-project/acm-cluster-service-provider/internal/service"
)

// Compile-time interface check.
var _ oapigen.StrictServerInterface = (*Handler)(nil)

// Handler implements StrictServerInterface by delegating to domain services.
type Handler struct {
	healthChecker service.HealthChecker
}

// New creates a Handler with the given dependencies.
func New(healthChecker service.HealthChecker) *Handler {
	return &Handler{healthChecker: healthChecker}
}

// GetHealth delegates to the HealthChecker and returns the result.
func (h *Handler) GetHealth(ctx context.Context, _ oapigen.GetHealthRequestObject) (oapigen.GetHealthResponseObject, error) {
	result := h.healthChecker.Check(ctx)
	return oapigen.GetHealth200JSONResponse(result), nil
}

// ListClusters is a stub for future implementation.
func (h *Handler) ListClusters(_ context.Context, _ oapigen.ListClustersRequestObject) (oapigen.ListClustersResponseObject, error) {
	return nil, nil
}

// CreateCluster is a stub for future implementation.
func (h *Handler) CreateCluster(_ context.Context, _ oapigen.CreateClusterRequestObject) (oapigen.CreateClusterResponseObject, error) {
	return nil, nil
}

// DeleteCluster is a stub for future implementation.
func (h *Handler) DeleteCluster(_ context.Context, _ oapigen.DeleteClusterRequestObject) (oapigen.DeleteClusterResponseObject, error) {
	return nil, nil
}

// GetCluster is a stub for future implementation.
func (h *Handler) GetCluster(_ context.Context, _ oapigen.GetClusterRequestObject) (oapigen.GetClusterResponseObject, error) {
	return nil, nil
}
