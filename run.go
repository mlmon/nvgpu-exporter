package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Run initializes metrics, starts collectors, and exposes the Prometheus HTTP handler.
func Run(addr *string, collectionInterval *time.Duration, devices Devices) error {
	log.Printf("Starting fabric health collector %v-%v\n", version, commit)

	err := initExporterInfo(devices, version, commit)
	if err != nil {

		return fmt.Errorf("failed to initialize exporter metrics: %w", err)
	}

	err = initGpuInfo(devices)
	if err != nil {

		return fmt.Errorf("failed to initialize gpu metrics: %w", err)
	}

	// Start fabric health collector
	startCollectors(devices, *collectionInterval)

	// Start Xid event collector
	if err := startXidEventCollector(devices); err != nil {
		return fmt.Errorf("failed to start xid event collector: %w", err)
	}

	logDeviceList(devices)

	http.Handle("/metrics", promhttp.Handler())

	log.Printf("Starting server on %s", *addr)
	if err := http.ListenAndServe(*addr, nil); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	return nil
}
