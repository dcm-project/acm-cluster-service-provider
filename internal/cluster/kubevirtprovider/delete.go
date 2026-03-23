package kubevirtprovider

import (
	"context"

	"github.com/dcm-project/acm-cluster-service-provider/internal/cluster"
	"github.com/dcm-project/acm-cluster-service-provider/internal/service"
	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (s *Service) Delete(ctx context.Context, id string) error {
	hc, err := s.findByInstanceID(ctx, id)
	if err != nil {
		return err
	}

	// Defense-in-depth: explicitly delete associated NodePools.
	// HyperShift cascades this via its finalizer, but we clean up proactively.
	if err := s.deleteNodePools(ctx, id); err != nil {
		return err
	}

	if err := s.client.Delete(ctx, hc); err != nil {
		return service.NewInternalError("failed to delete cluster", err)
	}

	return nil
}

func (s *Service) deleteNodePools(ctx context.Context, instanceID string) error {
	var npList hyperv1.NodePoolList
	if err := s.client.List(ctx, &npList,
		client.InNamespace(s.config.ClusterNamespace),
		client.MatchingLabels{cluster.LabelInstanceID: instanceID},
	); err != nil {
		return service.NewInternalError("failed to list node pools for deletion", err)
	}
	for i := range npList.Items {
		if err := s.client.Delete(ctx, &npList.Items[i]); err != nil {
			if !k8serrors.IsNotFound(err) {
				return service.NewInternalError("failed to delete node pool", err)
			}
		}
	}
	return nil
}
