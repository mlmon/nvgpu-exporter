package main

import (
	"flag"
	"log/slog"
	"os"
	"time"

	_ "go.uber.org/automaxprocs"
)

const (
	namespace = "nvgpu"
)

var (
	commit  = "unknown"
	version = "0.1.0"
)

func main() {
	addr := flag.String("addr", ":9400", "HTTP server address")
	collectionInterval := flag.Duration("collection-interval", 60*time.Second, "Interval for collecting GPU fabric health metrics")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	devices, shutdown, err := New(logger)
	if err != nil {
		logger.Error("failed to initialize NVML", "err", err)
		os.Exit(1)
	}
	defer shutdown()

	if err := Run(addr, collectionInterval, devices, logger); err != nil {
		logger.Error("exporter terminated", "err", err)
		os.Exit(1)
	}
}
