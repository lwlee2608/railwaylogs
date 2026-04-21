package railwaylog

import (
	"errors"
	"os"

	"github.com/lwlee2608/railwaylogs/internal/config"
	"github.com/lwlee2608/railwaylogs/pkg/railway"
)

// resolveLinked layers: CLI flags > config YAML > RAILWAY_*_ID env vars > ~/.railway/config.json.
func resolveLinked(cfg *config.Config, linkFile *railway.LinkedProject, projectFlag, envFlag, serviceFlag string) railway.LinkedProject {
	result := railway.LinkedProject{
		ProjectID:     projectFlag,
		EnvironmentID: envFlag,
		ServiceID:     serviceFlag,
	}

	fill := func(dst *string, src string) {
		if *dst == "" {
			*dst = src
		}
	}

	fill(&result.ProjectID, cfg.Railway.ProjectID)
	fill(&result.EnvironmentID, cfg.Railway.EnvironmentID)
	fill(&result.ServiceID, cfg.Railway.ServiceID)

	env := railway.LinkedFromEnv()
	fill(&result.ProjectID, env.ProjectID)
	fill(&result.EnvironmentID, env.EnvironmentID)
	fill(&result.ServiceID, env.ServiceID)

	if linkFile != nil {
		fill(&result.ProjectID, linkFile.ProjectID)
		fill(&result.EnvironmentID, linkFile.EnvironmentID)
		fill(&result.ServiceID, linkFile.ServiceID)
	}

	return result
}

// resolveAuth layers: env vars > ~/.railway/config.json.
func resolveAuth(linkFileAuth *railway.Auth) (*railway.Auth, error) {
	if a := railway.AuthFromEnv(); a != nil {
		return a, nil
	}
	if linkFileAuth != nil {
		return linkFileAuth, nil
	}
	return nil, errors.New("no Railway auth token; set RAILWAY_API_TOKEN / RAILWAY_TOKEN or run `railway login`")
}

func configPathHint() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "~/.config/railwaylog/config.yaml"
	}
	return home + "/.config/railwaylog/config.yaml"
}
