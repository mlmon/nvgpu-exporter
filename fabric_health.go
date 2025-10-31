package main

import (
	"errors"
	"log"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	fabricHealth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "fabric_health",
			Help:      "GPU fabric health status (1 = healthy/false, 0 = unhealthy/true).",
		},
		[]string{"UUID", "pci_bus_id", "health_field"},
	)
)

// collectFabricHealth collects GPU fabric health metrics for all devices
func collectFabricHealth(devices []nvml.Device) {
	for _, device := range devices {
		uuid, ret := device.GetUUID()
		if !errors.Is(ret, nvml.SUCCESS) {
			log.Printf("Failed to get UUID for device: %v", nvml.ErrorString(ret))
			continue
		}

		// Get PCI bus ID
		pciInfo, ret := device.GetPciInfo()
		if !errors.Is(ret, nvml.SUCCESS) {
			log.Printf("Failed to get PCI info for device %s: %v", uuid, nvml.ErrorString(ret))
			continue
		}
		pciBusId := pciBusIdToString(pciInfo.BusIdLegacy)

		// Get GPU fabric info - try V2 which includes health mask
		fabricInfo, ret := device.GetGpuFabricInfoV().V2()
		if !errors.Is(ret, nvml.SUCCESS) {
			log.Printf("Failed to get fabric info V2 for device %s: %v", uuid, nvml.ErrorString(ret))
			continue
		}

		// Extract health status bits from the health mask
		// Based on NVML documentation, the health mask contains various health indicators
		// We'll extract the common health fields using bit operations

		// Degraded bandwidth (bits 0-1)
		degradedBw := (fabricInfo.HealthMask >> 0) & 0x3
		fabricHealth.WithLabelValues(uuid, pciBusId, "degraded_bandwidth").Set(flagToGauge(degradedBw != 1))

		// Route recovery (bits 2-3)
		routeRecovery := (fabricInfo.HealthMask >> 2) & 0x3
		fabricHealth.WithLabelValues(uuid, pciBusId, "route_recovery").Set(flagToGauge(routeRecovery != 1))

		// Route unhealthy (bits 4-5)
		routeUnhealthy := (fabricInfo.HealthMask >> 4) & 0x3
		fabricHealth.WithLabelValues(uuid, pciBusId, "route_unhealthy").Set(flagToGauge(routeUnhealthy != 1))

		// Access timeout recovery (bits 6-7)
		accessTimeoutRecovery := (fabricInfo.HealthMask >> 6) & 0x3
		fabricHealth.WithLabelValues(uuid, pciBusId, "access_timeout_recovery").Set(flagToGauge(accessTimeoutRecovery != 1))
	}
}

// flagToGauge converts a boolean to a float64 for Prometheus gauges
// true (healthy/false) = 1.0, false (unhealthy/true) = 0.0
func flagToGauge(b bool) float64 {
	if b {
		return 1.0
	}
	return 0.0
}