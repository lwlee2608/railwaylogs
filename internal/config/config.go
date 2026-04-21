package config

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/lwlee2608/adder"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Log       LogConfig       `mapstructure:"log" yaml:"log"`
	Railway   RailwayConfig   `mapstructure:"railway" yaml:"railway"`
	Reconnect ReconnectConfig `mapstructure:"reconnect" yaml:"reconnect"`
}

type LogConfig struct {
	Level string `mapstructure:"level" yaml:"level"`
	Path  string `mapstructure:"path" yaml:"path"`
}

type RailwayConfig struct {
	ProjectID     string `mapstructure:"project_id" yaml:"project_id"`
	EnvironmentID string `mapstructure:"environment_id" yaml:"environment_id"`
	ServiceID     string `mapstructure:"service_id" yaml:"service_id"`
	HTTPEndpoint  string `mapstructure:"http_endpoint" yaml:"http_endpoint"`
	WSEndpoint    string `mapstructure:"ws_endpoint" yaml:"ws_endpoint"`
}

type ReconnectConfig struct {
	MaxAttempts    int `mapstructure:"max_attempts" yaml:"max_attempts"`
	InitialDelayMs int `mapstructure:"initial_delay_ms" yaml:"initial_delay_ms"`
	MaxDelayMs     int `mapstructure:"max_delay_ms" yaml:"max_delay_ms"`
}

func Load() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	configDir := filepath.Join(home, ".config", "railwaylog")
	configPath := filepath.Join(configDir, "config.yaml")

	if _, err := os.Stat(configPath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("stat config: %w", err)
		}
		if err := writeDefaultConfig(configPath); err != nil {
			return nil, fmt.Errorf("write default config: %w", err)
		}
	}

	a := adder.New()
	a.SetConfigName("config")
	a.SetConfigType("yaml")
	a.AddConfigPath(configDir)
	a.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	a.AutomaticEnv()

	cfg := &Config{}

	if err := a.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	if err := a.Unmarshal(cfg); err != nil {
		return nil, err
	}

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
		slog.Warn("backfill: read config", "path", path, "error", err)
		return false
	}

	var existingMap map[string]any
	if err := yaml.Unmarshal(existing, &existingMap); err != nil {
		slog.Warn("backfill: parse user config, skipping", "path", path, "error", err)
		return false
	}
	if existingMap == nil {
		existingMap = map[string]any{}
	}

	var defaultMap map[string]any
	if err := yaml.Unmarshal(defaultConfigYAML, &defaultMap); err != nil {
		slog.Warn("backfill: parse embedded defaults", "error", err)
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
		slog.Warn("backfill: marshal merged config", "error", err)
		return false
	}

	if bytes.Equal(bytes.TrimSpace(existing), bytes.TrimSpace(out)) {
		return false
	}

	if err := os.WriteFile(path, out, 0644); err != nil {
		slog.Warn("backfill: write merged config", "path", path, "error", err)
		return false
	}
	return true
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
