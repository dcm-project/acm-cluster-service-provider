package kubevirtprovider

import (
	"context"
	"fmt"
	"time"

	v1alpha1 "github.com/dcm-project/acm-cluster-service-provider/api/v1alpha1"
	"github.com/dcm-project/acm-cluster-service-provider/internal/cluster"
	"github.com/dcm-project/acm-cluster-service-provider/internal/service"
	"github.com/dcm-project/acm-cluster-service-provider/internal/util"
	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (s *Service) Create(ctx context.Context, id string, req v1alpha1.Cluster) (*v1alpha1.Cluster, error) {
	baseDomain := s.resolveBaseDomain(req)
	if baseDomain == "" {
		return nil, service.NewInvalidArgumentError("base_domain is required")
	}

	releaseImage, err := s.resolveReleaseImage(ctx, req)
	if err != nil {
		return nil, err
	}

	// Check for duplicate dcm-instance-id
	if err := s.checkDuplicateID(ctx, id); err != nil {
		return nil, err
	}

	labels := cluster.DCMLabels(id)

	hc := s.buildHostedCluster(req, baseDomain, releaseImage, labels)
	if err := s.client.Create(ctx, hc); err != nil {
		if k8serrors.IsAlreadyExists(err) {
			return nil, service.NewAlreadyExistsError(fmt.Sprintf("cluster with name %q already exists", req.Spec.Metadata.Name))
		}
		return nil, service.NewInternalError("failed to create cluster resources", err)
	}

	np := s.buildNodePool(req, releaseImage, labels)
	if err := s.client.Create(ctx, np); err != nil {
		// Rollback: delete orphan HostedCluster
		if delErr := s.client.Delete(ctx, hc); delErr != nil {
			return nil, service.NewInternalError(
				"failed to create node pool and rollback of hosted cluster failed",
				fmt.Errorf("create: %w, rollback: %v", err, delErr),
			)
		}
		return nil, service.NewInternalError("failed to create cluster resources", err)
	}

	now := time.Now()
	result := &v1alpha1.Cluster{
		Id:         util.Ptr(id),
		Path:       util.Ptr("clusters/" + id),
		Status:     util.Ptr(v1alpha1.ClusterStatusPENDING),
		CreateTime: &now,
		UpdateTime: &now,
		Spec:       req.Spec,
	}

	return result, nil
}

func (s *Service) resolveBaseDomain(req v1alpha1.Cluster) string {
	if req.Spec.ProviderHints != nil && req.Spec.ProviderHints.Acm != nil && req.Spec.ProviderHints.Acm.BaseDomain != nil {
		return *req.Spec.ProviderHints.Acm.BaseDomain
	}
	return s.config.BaseDomain
}

func (s *Service) resolveReleaseImage(ctx context.Context, req v1alpha1.Cluster) (string, error) {
	if req.Spec.ProviderHints != nil && req.Spec.ProviderHints.Acm != nil && req.Spec.ProviderHints.Acm.ReleaseImage != nil {
		return *req.Spec.ProviderHints.Acm.ReleaseImage, nil
	}
	resolver := cluster.NewVersionResolver(s.client)
	return resolver.Resolve(ctx, req.Spec.Version)
}

func (s *Service) checkDuplicateID(ctx context.Context, id string) error {
	var hcList hyperv1.HostedClusterList
	if err := s.client.List(ctx, &hcList,
		client.InNamespace(s.config.ClusterNamespace),
		client.MatchingLabels{cluster.LabelInstanceID: id},
	); err != nil {
		return service.NewInternalError("failed to check for duplicate cluster", err)
	}
	if len(hcList.Items) > 0 {
		return service.NewAlreadyExistsError("cluster with this ID already exists")
	}
	return nil
}

func (s *Service) buildHostedCluster(req v1alpha1.Cluster, baseDomain, releaseImage string, labels map[string]string) *hyperv1.HostedCluster {
	return &hyperv1.HostedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Spec.Metadata.Name,
			Namespace: s.config.ClusterNamespace,
			Labels:    labels,
		},
		Spec: hyperv1.HostedClusterSpec{
			Platform: hyperv1.PlatformSpec{
				Type: hyperv1.KubevirtPlatform,
			},
			Release: hyperv1.Release{
				Image: releaseImage,
			},
			DNS: hyperv1.DNSSpec{
				BaseDomain: baseDomain,
			},
		},
	}
}

func (s *Service) buildNodePool(req v1alpha1.Cluster, releaseImage string, labels map[string]string) *hyperv1.NodePool {
	replicas := int32(req.Spec.Nodes.Workers.Count)

	// Errors ignored: OpenAPI middleware validates format (^[1-9][0-9]*(MB|GB|TB)$)
	memory, _ := cluster.ParseDCMMemory(req.Spec.Nodes.Workers.Memory)
	storage, _ := cluster.ParseDCMMemory(req.Spec.Nodes.Workers.Storage)

	return &hyperv1.NodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Spec.Metadata.Name,
			Namespace: s.config.ClusterNamespace,
			Labels:    labels,
		},
		Spec: hyperv1.NodePoolSpec{
			ClusterName: req.Spec.Metadata.Name,
			Replicas:    &replicas,
			Release: hyperv1.Release{
				Image: releaseImage,
			},
			Platform: hyperv1.NodePoolPlatform{
				Type: hyperv1.KubevirtPlatform,
				Kubevirt: &hyperv1.KubevirtNodePoolPlatform{
					Compute: &hyperv1.KubevirtCompute{
						Memory: &memory,
						Cores:  util.Ptr(uint32(req.Spec.Nodes.Workers.Cpu)),
					},
					RootVolume: &hyperv1.KubevirtRootVolume{
						KubevirtVolume: hyperv1.KubevirtVolume{
							Type: hyperv1.KubevirtVolumeTypePersistent,
							Persistent: &hyperv1.KubevirtPersistentVolume{
								Size: &storage,
							},
						},
					},
				},
			},
		},
	}
}
