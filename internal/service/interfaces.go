package service

import (
	"context"

	v1alpha1 "github.com/dcm-project/acm-cluster-service-provider/api/v1alpha1"
)

// HealthChecker performs dependency health checks and returns the SP health status.
// The returned Health always has all required fields populated.
// The method never returns an error — unhealthy dependencies are reported via
// the Health.Status field ("unhealthy"), not via a Go error.
type HealthChecker interface {
	Check(ctx context.Context) v1alpha1.Health
}
