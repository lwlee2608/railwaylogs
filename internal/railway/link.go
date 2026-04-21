package railway

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type TokenKind int

const (
	TokenBearer TokenKind = iota
	TokenProjectAccess
)

type Auth struct {
	Token string
	Kind  TokenKind
}

type LinkedProject struct {
	ProjectID     string
	EnvironmentID string
	ServiceID     string
}

type rawConfig struct {
	Projects map[string]rawLinkedProject `json:"projects"`
	User     rawUser                     `json:"user"`
}

type rawLinkedProject struct {
	ProjectPath string `json:"projectPath"`
	Project     string `json:"project"`
	Environment string `json:"environment"`
	Service     string `json:"service"`
}

type rawUser struct {
	Token       string `json:"token"`
	AccessToken string `json:"accessToken"`
}

// Load reads Railway's own ~/.railway/config.json for a linked-project entry
// (resolved by cwd or ancestor) and the stored auth token. Either value may be
// empty; callers layer CLI flags and env vars on top.
func Load() (*LinkedProject, *Auth, error) {
	rc, err := readRailwayConfig()
	if err != nil {
		return nil, nil, err
	}

	var linked *LinkedProject
	if p := findByCwd(rc); p != nil {
		linked = &LinkedProject{
			ProjectID:     p.Project,
			EnvironmentID: p.Environment,
			ServiceID:     p.Service,
		}
	}

	var auth *Auth
	switch {
	case rc.User.AccessToken != "":
		auth = &Auth{Token: rc.User.AccessToken, Kind: TokenBearer}
	case rc.User.Token != "":
		auth = &Auth{Token: rc.User.Token, Kind: TokenBearer}
	}

	return linked, auth, nil
}

func readRailwayConfig() (*rawConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".railway", "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &rawConfig{}, nil
		}
		return nil, err
	}
	var rc rawConfig
	if err := json.Unmarshal(data, &rc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &rc, nil
}

func findByCwd(rc *rawConfig) *rawLinkedProject {
	if len(rc.Projects) == 0 {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	for dir := cwd; ; {
		if p, ok := rc.Projects[dir]; ok {
			return &p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}

// AuthFromEnv returns env-var-sourced auth if present: RAILWAY_TOKEN (project
// access) takes precedence over RAILWAY_API_TOKEN (bearer).
func AuthFromEnv() *Auth {
	if tok := os.Getenv("RAILWAY_TOKEN"); tok != "" {
		return &Auth{Token: tok, Kind: TokenProjectAccess}
	}
	if tok := os.Getenv("RAILWAY_API_TOKEN"); tok != "" {
		return &Auth{Token: tok, Kind: TokenBearer}
	}
	return nil
}

// LinkedFromEnv returns a partial LinkedProject from RAILWAY_*_ID env vars.
func LinkedFromEnv() LinkedProject {
	return LinkedProject{
		ProjectID:     os.Getenv("RAILWAY_PROJECT_ID"),
		EnvironmentID: os.Getenv("RAILWAY_ENVIRONMENT_ID"),
		ServiceID:     os.Getenv("RAILWAY_SERVICE_ID"),
	}
}
