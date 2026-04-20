package cluster

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"

	v1alpha1 "github.com/dcm-project/acm-cluster-service-provider/api/v1alpha1"
	"github.com/dcm-project/acm-cluster-service-provider/internal/config"
	"github.com/dcm-project/acm-cluster-service-provider/internal/registration"
	"github.com/dcm-project/acm-cluster-service-provider/internal/service"
	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// findByInstanceID lists HostedClusters matching the dcm-instance-id label
// in the given namespace. Returns the first match or a NotFoundError if none exist.
func findByInstanceID(ctx context.Context, c client.Client, namespace, id string) (*hyperv1.HostedCluster, error) {
	var hcList hyperv1.HostedClusterList
	if err := c.List(ctx, &hcList,
		client.InNamespace(namespace),
		client.MatchingLabels{LabelInstanceID: id},
	); err != nil {
		return nil, service.NewInternalError("failed to list clusters", err)
	}

	if len(hcList.Items) == 0 {
		return nil, service.NewNotFoundError(fmt.Sprintf("cluster %s not found", id))
	}

	return &hcList.Items[0], nil
}

// deleteNodePools deletes all NodePools matching the given instance ID
// in the specified namespace.
func deleteNodePools(ctx context.Context, c client.Client, namespace, instanceID string) error {
	var npList hyperv1.NodePoolList
	if err := c.List(ctx, &npList,
		client.InNamespace(namespace),
		client.MatchingLabels{LabelInstanceID: instanceID},
	); err != nil {
		return service.NewInternalError("failed to list node pools for deletion", err)
	}
	for i := range npList.Items {
		if err := c.Delete(ctx, &npList.Items[i]); err != nil {
			if !k8serrors.IsNotFound(err) {
				return service.NewInternalError("failed to delete node pool", err)
			}
		}
	}
	return nil
}

// GetCluster retrieves a cluster by instance ID.
func GetCluster(ctx context.Context, c client.Client, cfg config.ClusterConfig, id string) (*v1alpha1.Cluster, error) {
	hc, err := findByInstanceID(ctx, c, cfg.ClusterNamespace, id)
	if err != nil {
		return nil, err
	}

	result := HostedClusterToCluster(ctx, c, cfg, hc)
	return &result, nil
}

// ListClusters returns a paginated list of all managed clusters in the configured namespace.
func ListClusters(ctx context.Context, c client.Client, cfg config.ClusterConfig, pageSize int, pageToken string) (*v1alpha1.ClusterList, error) {
	var hcList hyperv1.HostedClusterList
	if err := c.List(ctx, &hcList,
		client.InNamespace(cfg.ClusterNamespace),
		client.MatchingLabels{
			LabelManagedBy:   ValueManagedBy,
			LabelServiceType: ValueServiceType,
		},
	); err != nil {
		return nil, service.NewInternalError("failed to list clusters", err)
	}

	sort.Slice(hcList.Items, func(i, j int) bool {
		return hcList.Items[i].Name < hcList.Items[j].Name
	})

	offset := 0
	if pageToken != "" {
		decoded, err := base64.StdEncoding.DecodeString(pageToken)
		if err != nil {
			return nil, service.NewInvalidArgumentError("invalid page_token")
		}
		v, err := strconv.Atoi(string(decoded))
		if err != nil || v < 0 {
			return nil, service.NewInvalidArgumentError("invalid page_token")
		}
		offset = v
	}

	total := len(hcList.Items)
	if offset > total {
		offset = total
	}
	end := offset + pageSize
	if end > total {
		end = total
	}
	page := hcList.Items[offset:end]

	clusters := make([]v1alpha1.Cluster, 0, len(page))
	for i := range page {
		clusters = append(clusters, HostedClusterToCluster(ctx, c, cfg, &page[i]))
	}

	result := &v1alpha1.ClusterList{
		Clusters: &clusters,
	}

	if end < total {
		nextToken := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%d", end)))
		result.NextPageToken = &nextToken
	}

	return result, nil
}

// UpdateCluster updates mutable fields of an existing cluster.
func UpdateCluster(ctx context.Context, c client.Client, cfg config.ClusterConfig, id string, req v1alpha1.Cluster, updateMask []string) (*v1alpha1.Cluster, error) {
	// Find the existing HostedCluster.
	hc, err := findByInstanceID(ctx, c, cfg.ClusterNamespace, id)
	if err != nil {
		return nil, err
	}

	// Determine which fields to update (all mutable fields if no mask specified).
	fieldMask := make(map[string]bool)
	if len(updateMask) > 0 {
		for _, field := range updateMask {
			fieldMask[field] = true
		}
	} else {
		// No mask means update all provided mutable fields.
		fieldMask["metadata.labels"] = true
		fieldMask["nodes.workers.count"] = true
		fieldMask["nodes.workers.cpu"] = true
		fieldMask["nodes.workers.memory"] = true
		fieldMask["nodes.workers.storage"] = true
		fieldMask["version"] = true
	}

	updated := false

	// Update metadata.labels if requested.
	if shouldUpdate(fieldMask, "metadata.labels") && req.Spec.Metadata.Labels != nil {
		if hc.Labels == nil {
			hc.Labels = make(map[string]string)
		}
		// Merge labels, preserving system labels.
		for k, v := range *req.Spec.Metadata.Labels {
			hc.Labels[k] = v
		}
		updated = true
	}

	// Update version if requested (triggers cluster upgrade).
	if shouldUpdate(fieldMask, "version") && req.Spec.Version != "" {
		// Resolve version to ClusterImageSet and release image.
		releaseImage, err := resolveReleaseImage(ctx, c, cfg, req.Spec.Version)
		if err != nil {
			return nil, err
		}
		hc.Spec.Release.Image = releaseImage
		updated = true
	}

	// Update HostedCluster if any changes were made.
	if updated {
		if err := c.Update(ctx, hc); err != nil {
			return nil, service.NewInternalError("failed to update cluster", err)
		}
	}

	// Update NodePool if worker node fields are requested.
	if shouldUpdateWorkerNodes(fieldMask) && req.Spec.Nodes != nil && req.Spec.Nodes.Workers != nil {
		if err := updateNodePools(ctx, c, hc.Namespace, id, req.Spec.Nodes.Workers, fieldMask); err != nil {
			return nil, err
		}
	}

	// Fetch the updated cluster state and return.
	return GetCluster(ctx, c, cfg, id)
}

// DeleteCluster deletes a cluster and its node pools by instance ID.
func DeleteCluster(ctx context.Context, c client.Client, cfg config.ClusterConfig, id string) error {
	hc, err := findByInstanceID(ctx, c, cfg.ClusterNamespace, id)
	if err != nil {
		return err
	}

	if err := deleteNodePools(ctx, c, hc.Namespace, id); err != nil {
		return err
	}

	if err := c.Delete(ctx, hc); err != nil {
		return service.NewInternalError("failed to delete cluster", err)
	}

	return nil
}

// resolveReleaseImage resolves a K8s version string to a release image.
func resolveReleaseImage(ctx context.Context, c client.Client, cfg config.ClusterConfig, version string) (string, error) {
	matrix := registration.CompatibilityMatrix(cfg.VersionMatrix)
	if len(matrix) == 0 {
		matrix = registration.DefaultCompatibilityMatrix
	}
	resolver := NewVersionResolver(c, matrix)
	return resolver.Resolve(ctx, version)
}

// shouldUpdate checks if a field should be updated based on the update mask.
func shouldUpdate(mask map[string]bool, field string) bool {
	if len(mask) == 0 {
		return true // No mask means update all.
	}
	return mask[field]
}

// shouldUpdateWorkerNodes checks if any worker node fields should be updated.
func shouldUpdateWorkerNodes(mask map[string]bool) bool {
	return shouldUpdate(mask, "nodes.workers.count") ||
		shouldUpdate(mask, "nodes.workers.cpu") ||
		shouldUpdate(mask, "nodes.workers.memory") ||
		shouldUpdate(mask, "nodes.workers.storage")
}

// updateNodePools updates worker NodePools for the given instance ID.
func updateNodePools(ctx context.Context, c client.Client, namespace, instanceID string, workers *v1alpha1.WorkerSpec, fieldMask map[string]bool) error {
	var npList hyperv1.NodePoolList
	if err := c.List(ctx, &npList,
		client.InNamespace(namespace),
		client.MatchingLabels{LabelInstanceID: instanceID},
	); err != nil {
		return service.NewInternalError("failed to list node pools", err)
	}

	if len(npList.Items) == 0 {
		// No NodePools to update (control-plane-only cluster).
		return nil
	}

	// Update the first NodePool (DCM clusters have a single worker pool).
	np := &npList.Items[0]

	if shouldUpdate(fieldMask, "nodes.workers.count") && workers.Count != nil {
		replicas := int32(*workers.Count)
		np.Spec.Replicas = &replicas
	}

	// Note: cpu, memory, storage updates would require modifying the NodePool template.
	// For now, we'll focus on the count field as that's the most common Day-2 operation.
	// Full implementation would update np.Spec.Platform.KubeVirt.RootVolume, etc.

	if err := c.Update(ctx, np); err != nil {
		return service.NewInternalError("failed to update node pool", err)
	}

	return nil
}
