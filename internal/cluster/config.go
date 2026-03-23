// Package cluster provides shared configuration, labels, and conversion utilities
// for cluster service provider implementations.
package cluster

// Config holds ACM cluster service configuration.
type Config struct {
	ClusterNamespace  string `env:"SP_CLUSTER_NAMESPACE,required"`
	BaseDomain        string `env:"SP_BASE_DOMAIN"`
	ConsoleURIPattern string `env:"SP_CONSOLE_URI_PATTERN" envDefault:"https://console-openshift-console.apps.{name}.{base_domain}"`
	VersionMatrixPath string `env:"SP_VERSION_MATRIX_PATH"`
}
