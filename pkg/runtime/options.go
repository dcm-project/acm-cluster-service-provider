package runtime

// Options configures Runtime construction and background lifecycle.
type Options struct {
	// Version is the application version reported by the health checker.
	// When empty, defaults to "0.0.1-dev".
	Version string

	// DisableRegistration skips SP registration. Use when the SP is registered
	// by an embedding host (e.g. environment-agent embedded SP mode).
	DisableRegistration bool

	// DisableMonitor skips the HostedCluster/NodePool status monitor and NATS
	// publisher. Rarely needed; intended for tests or minimal deployments.
	DisableMonitor bool
}

func (o Options) version() string {
	if o.Version != "" {
		return o.Version
	}
	return "0.0.1-dev"
}
