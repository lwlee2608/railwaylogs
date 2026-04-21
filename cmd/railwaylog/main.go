package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lwlee2608/railwaylog/internal/api"
	"github.com/lwlee2608/railwaylog/internal/config"
	"github.com/lwlee2608/railwaylog/internal/output"
	"github.com/lwlee2608/railwaylog/internal/railway"
)

var AppVersion = "dev"

func main() {
	if err := run(); err != nil {
		if !errors.Is(err, context.Canceled) {
			fmt.Fprintf(os.Stderr, "railwaylog: %v\n", err)
			os.Exit(1)
		}
	}
}

func run() error {
	var (
		serviceFlag     = flag.String("service", "", "Service ID (overrides config/link)")
		environmentFlag = flag.String("environment", "", "Environment ID (overrides config/link)")
		projectFlag     = flag.String("project", "", "Project ID (overrides config/link)")
		deploymentFlag  = flag.String("deployment", "", "Deployment ID (default: latest deployment)")
		showVersion     = flag.Bool("version", false, "Print version and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("railwaylog %s\n", AppVersion)
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	closeLogger, err := initLogger(cfg.Log.Level, cfg.Log.Path)
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	defer func() { _ = closeLogger() }()

	slog.Info("starting railwaylog", "version", AppVersion)

	railwayLinked, railwayAuth, err := railway.Load()
	if err != nil {
		slog.Warn("read ~/.railway/config.json", "error", err)
	}

	linked := resolveLinked(cfg, railwayLinked, *projectFlag, *environmentFlag, *serviceFlag)
	if linked.ProjectID == "" || linked.EnvironmentID == "" || linked.ServiceID == "" {
		return fmt.Errorf("missing project/environment/service — set in %s, use RAILWAY_*_ID env vars, pass --service/--environment/--project, or run `railway link`",
			configPathHint())
	}

	auth, err := resolveAuth(railwayAuth)
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	client := api.NewClient(auth, cfg.Railway.HTTPEndpoint, cfg.Railway.WSEndpoint)

	depID := *deploymentFlag
	if depID == "" {
		dep, err := client.LatestDeployment(ctx, linked.ServiceID, linked.EnvironmentID)
		if err != nil {
			return fmt.Errorf("find latest deployment: %w", err)
		}
		depID = dep.ID
		slog.Info("streaming deployment", "id", dep.ID, "status", dep.Status)
		fmt.Fprintf(os.Stderr, "railwaylog: streaming deployment %s (status=%s)\n", dep.ID, dep.Status)
	}

	writer := output.NewWriter(os.Stdout)

	retry := api.RetryConfig{
		MaxAttempts:  cfg.Reconnect.MaxAttempts,
		InitialDelay: time.Duration(cfg.Reconnect.InitialDelayMs) * time.Millisecond,
		MaxDelay:     time.Duration(cfg.Reconnect.MaxDelayMs) * time.Millisecond,
	}

	return client.StreamDeployLogs(ctx, depID, writer, retry)
}

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
