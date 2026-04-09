package handler

import (
	"fmt"
	"regexp"

	oapigen "github.com/dcm-project/acm-cluster-service-provider/internal/api/server"
)

var (
	memoryFormatRe = regexp.MustCompile(`^[1-9][0-9]*(MB|GB|TB)$`)
	clientIDRe     = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

// validateCreateRequest validates the CreateCluster request body.
func validateCreateRequest(body *oapigen.Cluster) error {
	if body.Spec.ServiceType != oapigen.ClusterSpecServiceTypeCluster {
		return fmt.Errorf("service_type must be %q", oapigen.ClusterSpecServiceTypeCluster)
	}
	if body.Spec.Version == "" {
		return fmt.Errorf("version is required")
	}
	if body.Spec.Nodes == nil {
		return fmt.Errorf("nodes is required")
	}
	if body.Spec.Nodes.Workers == nil {
		return fmt.Errorf("nodes.workers is required")
	}
	w := body.Spec.Nodes.Workers
	if w.Count != nil && *w.Count < 1 {
		return fmt.Errorf("nodes.workers.count must be >= 1")
	}
	if w.Memory != nil && !memoryFormatRe.MatchString(*w.Memory) {
		return fmt.Errorf("nodes.workers.memory must match format: [1-9][0-9]*(MB|GB|TB)")
	}
	if w.Storage != nil && !memoryFormatRe.MatchString(*w.Storage) {
		return fmt.Errorf("nodes.workers.storage must match format: [1-9][0-9]*(MB|GB|TB)")
	}
	if cp := body.Spec.Nodes.ControlPlane; cp != nil {
		if cp.Memory != nil && !memoryFormatRe.MatchString(*cp.Memory) {
			return fmt.Errorf("nodes.control_plane.memory must match format: [1-9][0-9]*(MB|GB|TB)")
		}
		if cp.Storage != nil && !memoryFormatRe.MatchString(*cp.Storage) {
			return fmt.Errorf("nodes.control_plane.storage must match format: [1-9][0-9]*(MB|GB|TB)")
		}
	}
	return nil
}

// validateClientID validates the client-specified ?id= parameter format.
func validateClientID(id string) error {
	if !clientIDRe.MatchString(id) {
		return fmt.Errorf("id must match pattern: ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$")
	}
	return nil
}

// validateMaxPageSize validates the max_page_size parameter is within bounds.
func validateMaxPageSize(size int32) error {
	if size < 1 || size > 100 {
		return fmt.Errorf("max_page_size must be between 1 and 100")
	}
	return nil
}
