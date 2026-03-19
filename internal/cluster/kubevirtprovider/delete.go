package kubevirtprovider

import (
	"context"

	"github.com/dcm-project/acm-cluster-service-provider/internal/service"
)

func (s *Service) Delete(ctx context.Context, id string) error {
	hc, err := s.findByInstanceID(ctx, id)
	if err != nil {
		return err
	}

	if err := s.client.Delete(ctx, hc); err != nil {
		return service.NewInternalError("failed to delete cluster", err)
	}

	return nil
}
