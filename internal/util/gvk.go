package util

import "k8s.io/apimachinery/pkg/runtime/schema"

// HyperShift / Hive API coordinates used across the service provider.
const (
	HiveGroup   = "hive.openshift.io"
	HiveVersion = "v1"
)

const (
	HypershiftGroup   = "hypershift.openshift.io"
	HypershiftVersion = "v1beta1"
)

// Pre-built GVKs for unstructured lookups.
var (
	HostedClusterGVK = schema.GroupVersionKind{
		Group: HypershiftGroup, Version: HypershiftVersion, Kind: "HostedCluster",
	}
	HostedClusterListGVK = schema.GroupVersionKind{
		Group: HypershiftGroup, Version: HypershiftVersion, Kind: "HostedClusterList",
	}
	ClusterImageSetGVK = schema.GroupVersionKind{
		Group: HiveGroup, Version: HiveVersion, Kind: "ClusterImageSet",
	}
	ClusterImageSetListGVK = schema.GroupVersionKind{
		Group: HiveGroup, Version: HiveVersion, Kind: "ClusterImageSetList",
	}
)
