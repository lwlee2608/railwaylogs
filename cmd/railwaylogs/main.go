package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/lwlee2608/railwaylogs/internal/railwaylog"
)

var AppVersion = "dev"

func main() {
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
		return
	}

	err := railwaylog.Run(railwaylog.Options{
		Version:     AppVersion,
		Project:     *projectFlag,
		Environment: *environmentFlag,
		Service:     *serviceFlag,
		Deployment:  *deploymentFlag,
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "railwaylog: %v\n", err)
		os.Exit(1)
	}
}
