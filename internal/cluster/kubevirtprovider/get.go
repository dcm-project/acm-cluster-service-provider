package kubevirtprovider

import (
	"context"

	v1alpha1 "github.com/dcm-project/acm-cluster-service-provider/api/v1alpha1"
)

func (s *Service) Get(ctx context.Context, id string) (*v1alpha1.Cluster, error) {
	hc, err := s.findByInstanceID(ctx, id)
	if err != nil {
		return nil, err
	}

	result := s.hostedClusterToCluster(ctx, hc)
	return &result, nil
}
