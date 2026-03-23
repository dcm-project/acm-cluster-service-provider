package cluster

import (
	"context"

	v1alpha1 "github.com/dcm-project/acm-cluster-service-provider/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ResolveBaseDomain returns the base domain from request hints or config fallback.
func ResolveBaseDomain(req v1alpha1.Cluster, configDefault string) string {
	if req.Spec.ProviderHints != nil && req.Spec.ProviderHints.Acm != nil && req.Spec.ProviderHints.Acm.BaseDomain != nil {
		return *req.Spec.ProviderHints.Acm.BaseDomain
	}
	return configDefault
}

// ResolveReleaseImage returns the release image from hints or resolves via ClusterImageSet.
func ResolveReleaseImage(ctx context.Context, c client.Client, req v1alpha1.Cluster) (string, error) {
	if req.Spec.ProviderHints != nil && req.Spec.ProviderHints.Acm != nil && req.Spec.ProviderHints.Acm.ReleaseImage != nil {
		return *req.Spec.ProviderHints.Acm.ReleaseImage, nil
	}
	resolver := NewVersionResolver(c)
	return resolver.Resolve(ctx, req.Spec.Version)
}
