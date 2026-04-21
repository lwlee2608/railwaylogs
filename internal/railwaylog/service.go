package railwaylog

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lwlee2608/railwaylogs/internal/config"
	"github.com/lwlee2608/railwaylogs/internal/output"
	"github.com/lwlee2608/railwaylogs/pkg/railway"
)

type Options struct {
	Version     string
	Project     string
	Environment string
	Service     string
	Deployment  string
}

func Run(opts Options) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	closeLogger, err := initLogger(cfg.Log.Level, cfg.Log.Path)
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	defer func() { _ = closeLogger() }()

	slog.Info("starting railwaylog", "version", opts.Version)

	railwayLinked, railwayAuth, err := railway.Load()
	if err != nil {
		slog.Warn("read ~/.railway/config.json", "error", err)
	}

	linked := resolveLinked(cfg, railwayLinked, opts.Project, opts.Environment, opts.Service)
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

	client := railway.NewClient(auth, cfg.Railway.HTTPEndpoint, cfg.Railway.WSEndpoint)

	depID := opts.Deployment
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

	retry := railway.RetryConfig{
		MaxAttempts:  cfg.Reconnect.MaxAttempts,
		InitialDelay: time.Duration(cfg.Reconnect.InitialDelayMs) * time.Millisecond,
		MaxDelay:     time.Duration(cfg.Reconnect.MaxDelayMs) * time.Millisecond,
	}

	return client.StreamDeployLogs(ctx, depID, writer, retry)
}
