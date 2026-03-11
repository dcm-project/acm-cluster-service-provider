package config

import (
	"fmt"
	"time"

	env "github.com/caarlos0/env/v11"
)

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	BindAddress     string        `env:"SP_SERVER_ADDRESS"          envDefault:":8080"`
	ShutdownTimeout time.Duration `env:"SP_SERVER_SHUTDOWN_TIMEOUT" envDefault:"15s"`
	RequestTimeout  time.Duration `env:"SP_SERVER_REQUEST_TIMEOUT"   envDefault:"30s"`
	ReadTimeout     time.Duration `env:"SP_SERVER_READ_TIMEOUT"     envDefault:"15s"`
	WriteTimeout    time.Duration `env:"SP_SERVER_WRITE_TIMEOUT"    envDefault:"15s"`
	IdleTimeout     time.Duration `env:"SP_SERVER_IDLE_TIMEOUT"     envDefault:"60s"`
}

// RegistrationConfig holds DCM registration settings.
type RegistrationConfig struct {
	DCMRegistrationURL         string        `env:"DCM_REGISTRATION_URL,required"`
	ProviderName               string        `env:"SP_NAME"                          envDefault:"acm-cluster-sp"`
	ProviderEndpoint           string        `env:"SP_ENDPOINT,required"`
	RegistrationInitialBackoff time.Duration `env:"SP_REGISTRATION_INITIAL_BACKOFF"  envDefault:"1s"`
	RegistrationMaxBackoff     time.Duration `env:"SP_REGISTRATION_MAX_BACKOFF"      envDefault:"5m"`
	VersionCheckInterval       time.Duration `env:"SP_VERSION_CHECK_INTERVAL"        envDefault:"5m"`
	ProviderDisplayName        string        `env:"SP_DISPLAY_NAME"                  envDefault:""`
	ProviderRegion             string        `env:"SP_REGION"                        envDefault:""`
	ProviderZone               string        `env:"SP_ZONE"                          envDefault:""`
}

// Config is the root configuration for the service provider.
type Config struct {
	Server       ServerConfig
	Registration RegistrationConfig
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("loading configuration: %w", err)
	}
	return cfg, nil
}
