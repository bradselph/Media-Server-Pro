package main

import (
	"flag"
	"fmt"
	"os"

	"media-server-pro/internal/logger"
	"media-server-pro/pkg/mediaserver"
)

// Version and BuildDate are set at build time via -ldflags:
//
//	go build -ldflags "-X main.Version=4.1.0 -X main.BuildDate=2026-02-26" ./cmd/server
var (
	Version   = "0.125.0"
	BuildDate = ""
)

func main() {
	var (
		configPath = flag.String("config", "config.json", "Path to config file")
		logLevel   = flag.String("log-level", "info", "Log level: debug, info, warn, error")
		showVer    = flag.Bool("version", false, "Show version and exit")
	)
	flag.Parse()

	if *showVer {
		if BuildDate != "" {
			fmt.Printf("Media Server Pro v%s (built %s)\n", Version, BuildDate)
		} else {
			fmt.Printf("Media Server Pro v%s\n", Version)
		}
		os.Exit(0)
	}

	var level logger.Level
	switch *logLevel {
	case "debug":
		level = logger.DEBUG
	case "warn":
		level = logger.WARN
	case "error":
		level = logger.ERROR
	default:
		level = logger.INFO
	}

	srv, err := mediaserver.New(
		mediaserver.WithConfigPath(*configPath),
		mediaserver.WithLogLevel(level),
		mediaserver.WithVersion(Version),
		mediaserver.WithBuildDate(BuildDate),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create server: %v\n", err)
		os.Exit(1)
	}

	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}
