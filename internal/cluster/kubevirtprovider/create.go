package kubevirtprovider

import (
	"context"

	v1alpha1 "github.com/dcm-project/acm-cluster-service-provider/api/v1alpha1"
	"github.com/dcm-project/acm-cluster-service-provider/internal/cluster"
	"github.com/dcm-project/acm-cluster-service-provider/internal/util"
	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (s *Service) Create(ctx context.Context, id string, req v1alpha1.Cluster) (*v1alpha1.Cluster, error) {
	return cluster.CreateCluster(ctx, s.client, s.config, id, req, s)
}

// BuildHostedCluster builds a KubeVirt-platform HostedCluster.
// control_plane.count and control_plane.storage are intentionally not mapped:
// HyperShift manages CP pod HA (ControllerAvailabilityPolicy) and etcd storage
// internally — these DCM fields describe node-level resources that don't exist
// in the hosted control plane model.
func (s *Service) BuildHostedCluster(req v1alpha1.Cluster, baseDomain, releaseImage string, labels map[string]string) *hyperv1.HostedCluster {
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
			Services: cluster.DefaultServicePublishingStrategies,
			Etcd: hyperv1.EtcdSpec{
				ManagementType: hyperv1.Managed,
				Managed: &hyperv1.ManagedEtcdSpec{
					Storage: hyperv1.ManagedEtcdStorageSpec{
						Type: hyperv1.PersistentVolumeEtcdStorage,
					},
				},
			},
		},
	}
}

func (s *Service) BuildNodePool(req v1alpha1.Cluster, releaseImage string, labels map[string]string) *hyperv1.NodePool {
	np := &hyperv1.NodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Spec.Metadata.Name,
			Namespace: s.config.ClusterNamespace,
			Labels:    labels,
		},
		Spec: hyperv1.NodePoolSpec{
			ClusterName: req.Spec.Metadata.Name,
			Release: hyperv1.Release{
				Image: releaseImage,
			},
			Platform: hyperv1.NodePoolPlatform{
				Type: hyperv1.KubevirtPlatform,
			},
		},
	}

	if req.Spec.Nodes != nil && req.Spec.Nodes.Workers != nil {
		w := req.Spec.Nodes.Workers
		if w.Count != nil {
			replicas := int32(*w.Count)
			np.Spec.Replicas = &replicas
		}

		kvPlatform := &hyperv1.KubevirtNodePoolPlatform{}
		hasCompute := false
		if w.Memory != nil {
			memory, _ := cluster.ParseDCMMemory(*w.Memory)
			kvPlatform.Compute = &hyperv1.KubevirtCompute{Memory: &memory}
			hasCompute = true
		}
		if w.Cpu != nil {
			if kvPlatform.Compute == nil {
				kvPlatform.Compute = &hyperv1.KubevirtCompute{}
			}
			kvPlatform.Compute.Cores = util.Ptr(uint32(*w.Cpu))
			hasCompute = true
		}
		if w.Storage != nil {
			storage, _ := cluster.ParseDCMMemory(*w.Storage)
			kvPlatform.RootVolume = &hyperv1.KubevirtRootVolume{
				KubevirtVolume: hyperv1.KubevirtVolume{
					Type: hyperv1.KubevirtVolumeTypePersistent,
					Persistent: &hyperv1.KubevirtPersistentVolume{
						Size: &storage,
					},
				},
			}
			hasCompute = true
		}
		if hasCompute {
			np.Spec.Platform.Kubevirt = kvPlatform
		}
	}

	return np
}
