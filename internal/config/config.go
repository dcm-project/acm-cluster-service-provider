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
	RequestTimeout  time.Duration `env:"SP_REQUEST_TIMEOUT"         envDefault:"30s"`
	ReadTimeout     time.Duration `env:"SP_SERVER_READ_TIMEOUT"     envDefault:"15s"`
	WriteTimeout    time.Duration `env:"SP_SERVER_WRITE_TIMEOUT"    envDefault:"15s"`
	IdleTimeout     time.Duration `env:"SP_SERVER_IDLE_TIMEOUT"     envDefault:"60s"`
}

// Config is the root configuration for the service provider.
type Config struct {
	Server ServerConfig
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("loading configuration: %w", err)
	}
	return cfg, nil
}
