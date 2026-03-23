// Package kubevirtprovider implements service.ClusterService for the KubeVirt platform.
package kubevirtprovider

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"strconv"
	"strings"

	v1alpha1 "github.com/dcm-project/acm-cluster-service-provider/api/v1alpha1"
	"github.com/dcm-project/acm-cluster-service-provider/internal/cluster"
	"github.com/dcm-project/acm-cluster-service-provider/internal/registration"
	"github.com/dcm-project/acm-cluster-service-provider/internal/service"
	"github.com/dcm-project/acm-cluster-service-provider/internal/service/status"
	"github.com/dcm-project/acm-cluster-service-provider/internal/util"
	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ service.ClusterService = (*Service)(nil)

// Service implements service.ClusterService for the KubeVirt platform.
type Service struct {
	client client.Client
	config cluster.Config
}

// New creates a new KubeVirt cluster service.
func New(c client.Client, cfg cluster.Config) *Service {
	return &Service{client: c, config: cfg}
}

// findByInstanceID lists HostedClusters matching the dcm-instance-id label.
// Returns the first match or a NotFoundError if none exist.
func (s *Service) findByInstanceID(ctx context.Context, id string) (*hyperv1.HostedCluster, error) {
	var hcList hyperv1.HostedClusterList
	if err := s.client.List(ctx, &hcList,
		client.InNamespace(s.config.ClusterNamespace),
		client.MatchingLabels{cluster.LabelInstanceID: id},
	); err != nil {
		return nil, service.NewInternalError("failed to list clusters", err)
	}

	if len(hcList.Items) == 0 {
		return nil, service.NewNotFoundError(fmt.Sprintf("cluster %s not found", id))
	}

	return &hcList.Items[0], nil
}

// hostedClusterToCluster converts a HostedCluster to a v1alpha1.Cluster.
func (s *Service) hostedClusterToCluster(ctx context.Context, hc *hyperv1.HostedCluster) v1alpha1.Cluster {
	instanceID := hc.Labels[cluster.LabelInstanceID]
	clusterStatus := status.MapConditionsToStatus(hc.Status.Conditions, hc.DeletionTimestamp)

	c := v1alpha1.Cluster{
		Id:     util.Ptr(instanceID),
		Path:   util.Ptr("clusters/" + instanceID),
		Status: &clusterStatus,
		Spec: v1alpha1.ClusterSpec{
			Metadata: v1alpha1.ClusterMetadata{
				Name: hc.Name,
			},
			ServiceType: v1alpha1.ClusterSpecServiceTypeCluster,
			Version:     cluster.ReleaseImageToK8sVersion(hc.Spec.Release.Image, registration.DefaultCompatibilityMatrix),
		},
	}

	if !hc.CreationTimestamp.IsZero() {
		t := hc.CreationTimestamp.Time
		c.CreateTime = &t
	}

	// UpdateTime: latest condition LastTransitionTime, or CreateTime if no conditions
	if len(hc.Status.Conditions) > 0 {
		latest := hc.Status.Conditions[0].LastTransitionTime.Time
		for _, cond := range hc.Status.Conditions[1:] {
			if cond.LastTransitionTime.Time.After(latest) {
				latest = cond.LastTransitionTime.Time
			}
		}
		c.UpdateTime = &latest
	} else if c.CreateTime != nil {
		c.UpdateTime = c.CreateTime
	}

	// Populate credentials for READY and UNAVAILABLE statuses
	if clusterStatus == v1alpha1.ClusterStatusREADY || clusterStatus == v1alpha1.ClusterStatusUNAVAILABLE {
		c.ApiEndpoint = buildAPIEndpoint(hc)
		c.ConsoleUri = s.buildConsoleURI(hc)
		c.Kubeconfig = s.extractKubeconfig(ctx, hc)
	}

	return c
}

// extractKubeconfig reads the kubeconfig Secret referenced by the HostedCluster.
// Returns base64-encoded data or nil on graceful degradation.
func (s *Service) extractKubeconfig(ctx context.Context, hc *hyperv1.HostedCluster) *string {
	if hc.Status.KubeConfig == nil || hc.Status.KubeConfig.Name == "" {
		return nil
	}

	secret := &corev1.Secret{}
	if err := s.client.Get(ctx, types.NamespacedName{
		Name:      hc.Status.KubeConfig.Name,
		Namespace: hc.Namespace,
	}, secret); err != nil {
		return nil
	}

	data, ok := secret.Data["kubeconfig"]
	if !ok || len(data) == 0 {
		return nil
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return &encoded
}

// buildConsoleURI constructs the console URI from the config pattern.
func (s *Service) buildConsoleURI(hc *hyperv1.HostedCluster) *string {
	if hc.Spec.DNS.BaseDomain == "" {
		return nil
	}
	uri := s.config.ConsoleURIPattern
	uri = strings.ReplaceAll(uri, "{name}", hc.Name)
	uri = strings.ReplaceAll(uri, "{base_domain}", hc.Spec.DNS.BaseDomain)
	return &uri
}

// buildAPIEndpoint constructs "https://{host}:{port}" from HC status.
func buildAPIEndpoint(hc *hyperv1.HostedCluster) *string {
	if hc.Status.ControlPlaneEndpoint.Host == "" {
		return nil
	}
	endpoint := "https://" + net.JoinHostPort(
		hc.Status.ControlPlaneEndpoint.Host,
		strconv.FormatInt(int64(hc.Status.ControlPlaneEndpoint.Port), 10),
	)
	return &endpoint
}
