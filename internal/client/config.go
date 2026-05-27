// Package client provides a thin authenticated HTTP client for the Artifactory REST API.
package client

import (
	"fmt"

	"github.com/spf13/viper"
)

// Config holds the Artifactory connection settings loaded from environment variables.
type Config struct {
	URL     string
	Token   string
	Verbose bool
}

// NewConfig reads ARTIFACTORY_URL and ARTIFACTORY_TOKEN from the environment.
func NewConfig() (Config, error) {
	viper.SetEnvPrefix("artifactory")
	if err := viper.BindEnv("url"); err != nil {
		return Config{}, err
	}
	if err := viper.BindEnv("token"); err != nil {
		return Config{}, err
	}

	cfg := Config{
		URL:   viper.GetString("url"),
		Token: viper.GetString("token"),
	}
	if cfg.URL == "" {
		return Config{}, fmt.Errorf("ARTIFACTORY_URL is not set")
	}
	if cfg.Token == "" {
		return Config{}, fmt.Errorf("ARTIFACTORY_TOKEN is not set")
	}
	return cfg, nil
}
