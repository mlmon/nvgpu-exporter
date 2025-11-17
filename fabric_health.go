package main

import (
	"errors"
	"fmt"
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
		[]string{"UUID", "pci_bus_id", "clique_id", "cluster_uuid", "health_field"},
	)

	fabricState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "fabric_state",
			Help:      "GPU fabric state (0=not_supported, 1=not_started, 2=in_progress, 3=completed).",
		},
		[]string{"UUID", "pci_bus_id", "clique_id", "cluster_uuid"},
	)

	fabricStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "fabric_status",
			Help:      "GPU fabric status code.",
		},
		[]string{"UUID", "pci_bus_id", "clique_id", "cluster_uuid"},
	)

	fabricHealthSummary = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "fabric_health_summary",
			Help:      "GPU fabric health summary (0=not_supported, 1=healthy, 2=unhealthy, 3=limited_capacity).",
		},
		[]string{"UUID", "pci_bus_id", "clique_id", "cluster_uuid"},
	)

	fabricIncorrectConfig = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "fabric_incorrect_configuration",
			Help:      "GPU fabric incorrect configuration status (0=not_supported, 1=none, 2=incorrect_sysguid, 3=incorrect_chassis_sn, 4=no_partition, 5=insufficient_nvlinks).",
		},
		[]string{"UUID", "pci_bus_id", "clique_id", "cluster_uuid"},
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

		// Convert ClusterUUID from byte array to string
		clusterUUID := uuidBytesToString(fabricInfo.ClusterUuid)
		cliqueID := fmt.Sprintf("%d", fabricInfo.CliqueId)

		// Fabric state metric
		fabricState.WithLabelValues(uuid, pciBusId, cliqueID, clusterUUID).Set(float64(fabricInfo.State))

		// Fabric status metric
		fabricStatus.WithLabelValues(uuid, pciBusId, cliqueID, clusterUUID).Set(float64(fabricInfo.Status))

		// Extract health status bits from the health mask
		// Based on NVML documentation, the health mask contains various health indicators
		// We'll extract the common health fields using bit operations

		// Degraded bandwidth (bits 0-1)
		degradedBw := (fabricInfo.HealthMask >> 0) & 0x3
		fabricHealth.WithLabelValues(uuid, pciBusId, cliqueID, clusterUUID, "degraded_bandwidth").Set(flagToGauge(degradedBw != 1))

		// Route recovery (bits 2-3)
		routeRecovery := (fabricInfo.HealthMask >> 2) & 0x3
		fabricHealth.WithLabelValues(uuid, pciBusId, cliqueID, clusterUUID, "route_recovery").Set(flagToGauge(routeRecovery != 1))

		// Route unhealthy (bits 4-5)
		routeUnhealthy := (fabricInfo.HealthMask >> 4) & 0x3
		fabricHealth.WithLabelValues(uuid, pciBusId, cliqueID, clusterUUID, "route_unhealthy").Set(flagToGauge(routeUnhealthy != 1))

		// Access timeout recovery (bits 6-7)
		accessTimeoutRecovery := (fabricInfo.HealthMask >> 6) & 0x3
		fabricHealth.WithLabelValues(uuid, pciBusId, cliqueID, clusterUUID, "access_timeout_recovery").Set(flagToGauge(accessTimeoutRecovery != 1))

		// Incorrect configuration (bits 8-21)
		incorrectConfig := (fabricInfo.HealthMask >> 8) & 0x3FFF
		fabricIncorrectConfig.WithLabelValues(uuid, pciBusId, cliqueID, clusterUUID).Set(float64(incorrectConfig))

		// Calculate health summary based on all health mask fields
		healthSummary := calculateHealthSummary(degradedBw, routeRecovery, routeUnhealthy, accessTimeoutRecovery, incorrectConfig)
		fabricHealthSummary.WithLabelValues(uuid, pciBusId, cliqueID, clusterUUID).Set(float64(healthSummary))
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

// uuidBytesToString converts a 16-byte UUID array to a standard UUID string format
func uuidBytesToString(uuid [16]uint8) string {
	return fmt.Sprintf("%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		uuid[0], uuid[1], uuid[2], uuid[3],
		uuid[4], uuid[5],
		uuid[6], uuid[7],
		uuid[8], uuid[9],
		uuid[10], uuid[11], uuid[12], uuid[13], uuid[14], uuid[15])
}

// calculateHealthSummary determines the overall health summary based on individual health fields
// Returns: 0=not_supported, 1=healthy, 2=unhealthy, 3=limited_capacity
func calculateHealthSummary(degradedBw, routeRecovery, routeUnhealthy, accessTimeoutRecovery, incorrectConfig uint32) uint32 {
	// Check if all fields are not supported
	if degradedBw == nvml.GPU_FABRIC_HEALTH_MASK_DEGRADED_BW_NOT_SUPPORTED &&
		routeRecovery == nvml.GPU_FABRIC_HEALTH_MASK_ROUTE_RECOVERY_NOT_SUPPORTED &&
		routeUnhealthy == nvml.GPU_FABRIC_HEALTH_MASK_ROUTE_UNHEALTHY_NOT_SUPPORTED &&
		accessTimeoutRecovery == nvml.GPU_FABRIC_HEALTH_MASK_ACCESS_TIMEOUT_RECOVERY_NOT_SUPPORTED &&
		incorrectConfig == nvml.GPU_FABRIC_HEALTH_MASK_INCORRECT_CONFIGURATION_NOT_SUPPORTED {
		return nvml.GPU_FABRIC_HEALTH_SUMMARY_NOT_SUPPORTED
	}

	// Check for unhealthy conditions (any TRUE flag indicates an issue)
	if routeRecovery == nvml.GPU_FABRIC_HEALTH_MASK_ROUTE_RECOVERY_TRUE ||
		routeUnhealthy == nvml.GPU_FABRIC_HEALTH_MASK_ROUTE_UNHEALTHY_TRUE ||
		accessTimeoutRecovery == nvml.GPU_FABRIC_HEALTH_MASK_ACCESS_TIMEOUT_RECOVERY_TRUE ||
		(incorrectConfig != nvml.GPU_FABRIC_HEALTH_MASK_INCORRECT_CONFIGURATION_NOT_SUPPORTED &&
			incorrectConfig != nvml.GPU_FABRIC_HEALTH_MASK_INCORRECT_CONFIGURATION_NONE) {
		return nvml.GPU_FABRIC_HEALTH_SUMMARY_UNHEALTHY
	}

	// Check for degraded bandwidth (limited capacity)
	if degradedBw == nvml.GPU_FABRIC_HEALTH_MASK_DEGRADED_BW_TRUE {
		return nvml.GPU_FABRIC_HEALTH_SUMMARY_LIMITED_CAPACITY
	}

	// All checks pass, fabric is healthy
	return nvml.GPU_FABRIC_HEALTH_SUMMARY_HEALTHY
}