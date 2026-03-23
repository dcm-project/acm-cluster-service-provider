// Package baremetal implements the BareMetal (Agent) platform Create operation.
package baremetal

import (
	"github.com/dcm-project/acm-cluster-service-provider/internal/config"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Service implements Create for the BareMetal platform.
type Service struct {
	client client.Client
	config config.ClusterConfig
}

// New creates a new BareMetal cluster service.
func New(c client.Client, cfg config.ClusterConfig) *Service {
	return &Service{client: c, config: cfg}
}
