package config

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lwlee2608/adder"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Log       LogConfig       `mapstructure:"log"`
	Railway   RailwayConfig   `mapstructure:"railway"`
	Reconnect ReconnectConfig `mapstructure:"reconnect"`
}

type LogConfig struct {
	Level string `mapstructure:"level"`
	Path  string `mapstructure:"path"`
}

type RailwayConfig struct {
	ProjectID     string `mapstructure:"project_id"`
	EnvironmentID string `mapstructure:"environment_id"`
	ServiceID     string `mapstructure:"service_id"`
	HTTPEndpoint  string `mapstructure:"http_endpoint"`
	WSEndpoint    string `mapstructure:"ws_endpoint"`
}

type ReconnectConfig struct {
	MaxAttempts    int `mapstructure:"max_attempts"`
	InitialDelayMs int `mapstructure:"initial_delay_ms"`
	MaxDelayMs     int `mapstructure:"max_delay_ms"`
}

func Load() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	a := adder.New()
	a.SetConfigName("config")
	a.SetConfigType("yaml")
	a.AddConfigPath(filepath.Join(home, ".config", "railwaylog"))
	a.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	a.AutomaticEnv()

	cfg := &Config{}

	if err := a.ReadInConfig(); err != nil {
		if !strings.HasPrefix(err.Error(), "config file not found") {
			return nil, err
		}
		if err := writeDefaultConfig(filepath.Join(home, ".config", "railwaylog", "config.yaml")); err != nil {
			return nil, fmt.Errorf("write default config: %w", err)
		}
		if err := a.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("read default config: %w", err)
		}
	}

	if err := a.Unmarshal(cfg); err != nil {
		return nil, err
	}

	configPath := filepath.Join(home, ".config", "railwaylog", "config.yaml")
	if backfillDefaults(configPath) {
		if err := a.ReadInConfig(); err != nil {
			return nil, err
		}
		if err := a.Unmarshal(cfg); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

//go:embed default_config.yaml
var defaultConfigYAML []byte

func writeDefaultConfig(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, defaultConfigYAML, 0644)
}

func backfillDefaults(path string) bool {
	existing, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	var existingMap map[string]any
	if err := yaml.Unmarshal(existing, &existingMap); err != nil {
		return false
	}
	if existingMap == nil {
		existingMap = map[string]any{}
	}

	var defaultMap map[string]any
	if err := yaml.Unmarshal(defaultConfigYAML, &defaultMap); err != nil {
		return false
	}
	if defaultMap == nil {
		return false
	}

	if !mergeDefaults(existingMap, defaultMap) {
		return false
	}

	out, err := yaml.Marshal(existingMap)
	if err != nil {
		return false
	}

	if !bytes.Equal(bytes.TrimSpace(existing), bytes.TrimSpace(out)) {
		if err := os.WriteFile(path, out, 0644); err != nil {
			return false
		}
		return true
	}

	return false
}

func mergeDefaults(dst, src map[string]any) bool {
	changed := false
	for k, srcVal := range src {
		dstVal, exists := dst[k]
		if !exists {
			dst[k] = srcVal
			changed = true
			continue
		}
		dstMap, dstOk := dstVal.(map[string]any)
		srcMap, srcOk := srcVal.(map[string]any)
		if dstOk && srcOk {
			if mergeDefaults(dstMap, srcMap) {
				changed = true
			}
		}
	}
	return changed
}
