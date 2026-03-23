package cluster

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
)

// ParseDCMMemory converts a DCM-format memory/storage string (e.g. "16GB", "512MB", "2TB")
// to a K8s resource.Quantity. DCM uses decimal units; this strips the trailing "B"
// so K8s interprets "16GB" as "16G" (decimal gigabytes).
func ParseDCMMemory(s string) (resource.Quantity, error) {
	if s == "" {
		return resource.Quantity{}, fmt.Errorf("empty memory/storage value")
	}
	k8sValue := strings.TrimSuffix(s, "B")
	if k8sValue == s {
		return resource.Quantity{}, fmt.Errorf("unsupported memory/storage format: %s", s)
	}
	q, err := resource.ParseQuantity(k8sValue)
	if err != nil {
		return resource.Quantity{}, fmt.Errorf("invalid memory/storage value %q: %w", s, err)
	}
	return q, nil
}
