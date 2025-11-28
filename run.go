package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Run initializes metrics, starts collectors, and exposes the Prometheus HTTP handler.
func Run(addr *string, collectionInterval *time.Duration, devices Devices, logger *slog.Logger) error {
	logger.Info("starting nvgpu collector", "version", version, "commit", commit)

	gpuInfos, err := loadGpuInfos(devices)
	if err != nil {
		return fmt.Errorf("failed to preload gpu info: %w", err)
	}

	if err := initExporterInfo(devices, version, commit); err != nil {
		return fmt.Errorf("failed to initialize exporter metrics: %w", err)
	}

	if err := initGpuInfoWithCache(gpuInfos); err != nil {
		return fmt.Errorf("failed to initialize gpu metrics: %w", err)
	}

	// Start fabric health collector
	startCollectors(devices, *collectionInterval, gpuInfos, logger)

	// Start Xid event collector
	if err := startXidEventCollector(devices, logger); err != nil {
		return fmt.Errorf("failed to start xid event collector: %w", err)
	}

	logDeviceList(devices, logger)

	http.Handle("/metrics", promhttp.Handler())

	logger.Info("starting HTTP server", "addr", *addr)
	if err := http.ListenAndServe(*addr, nil); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	return nil
}
