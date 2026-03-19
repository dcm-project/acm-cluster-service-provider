package kubevirtprovider

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"

	v1alpha1 "github.com/dcm-project/acm-cluster-service-provider/api/v1alpha1"
	"github.com/dcm-project/acm-cluster-service-provider/internal/cluster"
	"github.com/dcm-project/acm-cluster-service-provider/internal/service"
	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (s *Service) List(ctx context.Context, pageSize int, pageToken string) (*v1alpha1.ClusterList, error) {
	var hcList hyperv1.HostedClusterList
	if err := s.client.List(ctx, &hcList,
		client.InNamespace(s.config.ClusterNamespace),
		client.MatchingLabels{
			cluster.LabelManagedBy:   cluster.ValueManagedBy,
			cluster.LabelServiceType: cluster.ValueServiceType,
		},
	); err != nil {
		return nil, service.NewInternalError("failed to list clusters", err)
	}

	// Sort by metadata.name ascending
	sort.Slice(hcList.Items, func(i, j int) bool {
		return hcList.Items[i].Name < hcList.Items[j].Name
	})

	// Decode page token → offset
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

	// Apply pagination
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
		clusters = append(clusters, s.hostedClusterToCluster(ctx, &page[i]))
	}

	result := &v1alpha1.ClusterList{
		Clusters: &clusters,
	}

	// Next page token
	if end < total {
		nextToken := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%d", end)))
		result.NextPageToken = &nextToken
	}

	return result, nil
}
