package main

import (
	"fmt"
	"log"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type GpuInfo struct {
	UUID                string
	PciBusId            string
	Name                string
	Brand               string
	Serial              string
	BoardId             string
	OemInforomVersion   string
	EccInforomVersion   string
	PowerInforomVersion string
	VbiosVersion        string
	InforomImageVersion string
}

type ExporterInfo struct {
	CudaVersion   string
	DriverVersion string
	NVMLVersion   string
}

type DeviceLister interface {
	GpuInfo(i int) (*GpuInfo, error)
	ExporterInfo() (*ExporterInfo, error)
}

func logDeviceList(devices Devices) {
	log.Printf("Found %d GPU device(s)\n", len(devices))

	for i := range devices {
		info, err := devices.GpuInfo(i)
		if err != nil {
			log.Fatalf("failed to get GPU info: %v", err)
		}

		log.Printf("Device %d: %s (UUID: %s)\n", i, info.Name, info.UUID)
	}
}

var exporterInfo = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "exporter_info",
		Help:      "Information about the nvgpu-exporter.",
	},
	[]string{"version", "driver_version", "nvml_version", "cuda_version"},
)

var gpuInfo = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "gpu_info",
		Help:      "GPU device information.",
	},
	[]string{"UUID", "pci_bus_id", "name", "brand", "serial", "board_id", "vbios_version", "oem_inforom_version", "ecc_inforom_version", "power_inforom_version", "inforom_image_version"},
)

func initExporterInfo(devices Devices, version string, commit string) error {
	info, err := devices.ExporterInfo()
	if err != nil {
		return err
	}

	// Set the exporter info metric
	exporterInfo.WithLabelValues(version+"-"+commit, info.DriverVersion, info.NVMLVersion, info.CudaVersion).Set(1)

	// Register the exporter info metric
	prometheus.MustRegister(exporterInfo)
	return nil
}

func initGpuInfo(devices Devices) error {
	for i := range devices {
		info, err := devices.GpuInfo(i)
		if err != nil {
			return fmt.Errorf("failed to get GPU info for device %d: %w", i, err)
		}

		// Set GPU info metric
		gpuInfo.WithLabelValues(
			info.UUID,
			info.PciBusId,
			info.Name,
			info.Brand,
			info.Serial,
			info.BoardId,
			info.VbiosVersion,
			info.OemInforomVersion,
			info.EccInforomVersion,
			info.PowerInforomVersion,
			info.InforomImageVersion,
		).Set(1)
	}

	// Register the GPU info metric
	prometheus.MustRegister(gpuInfo)

	return nil
}

// startCollectors starts a goroutine that periodically collects fabric health and NVLink error metrics
func startCollectors(devices Devices, interval time.Duration) {
	// Register the metrics
	prometheus.MustRegister(fabricHealth)
	prometheus.MustRegister(fabricState)
	prometheus.MustRegister(fabricStatus)
	prometheus.MustRegister(fabricHealthSummary)
	prometheus.MustRegister(fabricIncorrectConfig)
	prometheus.MustRegister(nvlinkErrors)

	// Start the collection goroutine
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Collect immediately on start
		collectFabricHealth(devices)
		collectNVLinkErrors(devices)

		// Then collect periodically
		for range ticker.C {
			collectFabricHealth(devices)
			collectNVLinkErrors(devices)
		}
	}()

	log.Printf("Started fabric health and NVLink error collector with interval: %v", interval)
}
