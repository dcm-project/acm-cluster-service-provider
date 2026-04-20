package handler_test

import (
	"context"
	"fmt"
	"time"

	v1alpha1 "github.com/dcm-project/acm-cluster-service-provider/api/v1alpha1"
	oapigen "github.com/dcm-project/acm-cluster-service-provider/internal/api/server"
	"github.com/dcm-project/acm-cluster-service-provider/internal/service"
	"github.com/dcm-project/acm-cluster-service-provider/internal/util"
)

// mockClusterService implements service.ClusterService with functional mock fields.
type mockClusterService struct {
	CreateFunc func(ctx context.Context, id string, cluster v1alpha1.Cluster) (*v1alpha1.Cluster, error)
	GetFunc    func(ctx context.Context, id string) (*v1alpha1.Cluster, error)
	ListFunc   func(ctx context.Context, pageSize int, pageToken string) (*v1alpha1.ClusterList, error)
	UpdateFunc func(ctx context.Context, id string, cluster v1alpha1.Cluster, updateMask []string) (*v1alpha1.Cluster, error)
	DeleteFunc func(ctx context.Context, id string) error
}

var _ service.ClusterService = (*mockClusterService)(nil)

func (m *mockClusterService) Create(ctx context.Context, id string, cluster v1alpha1.Cluster) (*v1alpha1.Cluster, error) {
	if m.CreateFunc == nil {
		panic("CreateFunc not set")
	}
	return m.CreateFunc(ctx, id, cluster)
}

func (m *mockClusterService) Get(ctx context.Context, id string) (*v1alpha1.Cluster, error) {
	if m.GetFunc == nil {
		panic("GetFunc not set")
	}
	return m.GetFunc(ctx, id)
}

func (m *mockClusterService) List(ctx context.Context, pageSize int, pageToken string) (*v1alpha1.ClusterList, error) {
	if m.ListFunc == nil {
		panic("ListFunc not set")
	}
	return m.ListFunc(ctx, pageSize, pageToken)
}

func (m *mockClusterService) Update(ctx context.Context, id string, cluster v1alpha1.Cluster, updateMask []string) (*v1alpha1.Cluster, error) {
	if m.UpdateFunc == nil {
		panic("UpdateFunc not set")
	}
	return m.UpdateFunc(ctx, id, cluster, updateMask)
}

func (m *mockClusterService) Delete(ctx context.Context, id string) error {
	if m.DeleteFunc == nil {
		panic("DeleteFunc not set")
	}
	return m.DeleteFunc(ctx, id)
}

// validClusterBody returns an oapigen.Cluster for use in CreateCluster request bodies.
func validClusterBody() oapigen.Cluster {
	return oapigen.Cluster{
		Spec: oapigen.ClusterSpec{
			Version:     "1.30",
			ServiceType: oapigen.ClusterSpecServiceTypeCluster,
			Metadata: oapigen.ClusterMetadata{
				Name: "test-cluster",
			},
			Nodes: &oapigen.ClusterNodes{
				ControlPlane: &oapigen.ControlPlaneSpec{
					Count:   util.Ptr(oapigen.N3),
					Cpu:     util.Ptr(4),
					Memory:  util.Ptr("16GB"),
					Storage: util.Ptr("120GB"),
				},
				Workers: &oapigen.WorkerSpec{
					Count:   util.Ptr(3),
					Cpu:     util.Ptr(8),
					Memory:  util.Ptr("32GB"),
					Storage: util.Ptr("500GB"),
				},
			},
		},
	}
}

// clusterResult simulates a successful service response for a cluster (v1alpha1 types).
func clusterResult(id string) *v1alpha1.Cluster {
	now := time.Now()
	return &v1alpha1.Cluster{
		Id:         util.Ptr(id),
		Path:       util.Ptr("/api/v1alpha1/clusters/" + id),
		Status:     util.Ptr(v1alpha1.ClusterStatusPENDING),
		CreateTime: &now,
		UpdateTime: &now,
		Spec: v1alpha1.ClusterSpec{
			Version:     "1.30",
			ServiceType: v1alpha1.ClusterSpecServiceTypeCluster,
			Metadata: v1alpha1.ClusterMetadata{
				Name: "test-cluster",
			},
			Nodes: &v1alpha1.ClusterNodes{
				ControlPlane: &v1alpha1.ControlPlaneSpec{
					Count:   util.Ptr(v1alpha1.N3),
					Cpu:     util.Ptr(4),
					Memory:  util.Ptr("16GB"),
					Storage: util.Ptr("120GB"),
				},
				Workers: &v1alpha1.WorkerSpec{
					Count:   util.Ptr(3),
					Cpu:     util.Ptr(8),
					Memory:  util.Ptr("32GB"),
					Storage: util.Ptr("500GB"),
				},
			},
		},
	}
}

// readyClusterResult returns a READY cluster with credentials populated (v1alpha1 types).
func readyClusterResult(id string) *v1alpha1.Cluster {
	result := clusterResult(id)
	result.Status = util.Ptr(v1alpha1.ClusterStatusREADY)
	result.ApiEndpoint = util.Ptr("https://api.cluster.example.com:6443")
	result.Kubeconfig = util.Ptr("base64-kubeconfig-data")
	result.ConsoleUri = util.Ptr("https://console.cluster.example.com")
	return result
}

// clusterListResult creates a ClusterList for testing (v1alpha1 types).
func clusterListResult(count int, nextToken string) *v1alpha1.ClusterList {
	clusters := make([]v1alpha1.Cluster, count)
	for i := range clusters {
		clusters[i] = *clusterResult(fmt.Sprintf("cluster-%d", i))
	}
	result := &v1alpha1.ClusterList{
		Clusters: &clusters,
	}
	if nextToken != "" {
		result.NextPageToken = util.Ptr(nextToken)
	}
	return result
}
