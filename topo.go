package main

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	gpuTopology = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "gpu_topology",
			Help:      "GPU topology information including affinity and connections.",
		},
		[]string{"UUID", "pci_bus_id", "gpu_id", "cpu_affinity", "numa_affinity", "gpu_numa_id", "peer_type", "peer_id", "connection"},
	)

	nicTopology = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "nic_topology",
			Help:      "NIC topology information showing connections to GPUs and other NICs.",
		},
		[]string{"nic_name", "nic_id", "peer_type", "peer_id", "connection"},
	)
)

// topologyLevel maps NVML GPU topology levels to human-readable strings
func topologyLevelToString(level nvml.GpuTopologyLevel) string {
	switch level {
	case nvml.TOPOLOGY_INTERNAL:
		return "INTERNAL"
	case nvml.TOPOLOGY_SINGLE:
		return "PIX" // Single PCIe switch
	case nvml.TOPOLOGY_MULTIPLE:
		return "PXB" // Multiple PCIe switches
	case nvml.TOPOLOGY_HOSTBRIDGE:
		return "PHB" // PCIe Host Bridge
	case nvml.TOPOLOGY_NODE:
		return "NODE" // PCIe + interconnect within NUMA node
	case nvml.TOPOLOGY_SYSTEM:
		return "SYS" // PCIe + SMP interconnect between NUMA nodes
	default:
		return "UNKNOWN"
	}
}

// formatCpuAffinity converts a CPU affinity bitmask to a string range (e.g., "0-69")
func formatCpuAffinity(cpus []uint) string {
	if len(cpus) == 0 {
		return "N/A"
	}

	// Convert bitmask to list of CPU IDs
	cpuList := make([]int, 0)
	for wordIdx, word := range cpus {
		// uint can be 32 or 64 bits depending on platform
		bitSize := 32
		if strconv.IntSize == 64 {
			bitSize = 64
		}
		for bit := 0; bit < bitSize; bit++ {
			if word&(1<<bit) != 0 {
				cpuList = append(cpuList, wordIdx*bitSize+bit)
			}
		}
	}

	if len(cpuList) == 0 {
		return "N/A"
	}

	// Find contiguous ranges
	ranges := make([]string, 0)
	start := cpuList[0]
	prev := cpuList[0]

	for i := 1; i < len(cpuList); i++ {
		if cpuList[i] != prev+1 {
			// End of range
			if start == prev {
				ranges = append(ranges, strconv.Itoa(start))
			} else {
				ranges = append(ranges, fmt.Sprintf("%d-%d", start, prev))
			}
			start = cpuList[i]
		}
		prev = cpuList[i]
	}

	// Add final range
	if start == prev {
		ranges = append(ranges, strconv.Itoa(start))
	} else {
		ranges = append(ranges, fmt.Sprintf("%d-%d", start, prev))
	}

	return strings.Join(ranges, ",")
}

// getNVLinkConnection returns the NVLink connection type (e.g., "NV18") or empty string
func getNVLinkConnection(device1, device2 nvml.Device) string {
	// Get NVLink connection count between two devices
	count := 0
	for link := 0; link < nvml.NVLINK_MAX_LINKS; link++ {
		remoteInfo, ret := device1.GetNvLinkRemotePciInfo(link)
		if !errors.Is(ret, nvml.SUCCESS) {
			continue
		}

		// Get PCI info for device2 to compare
		pciInfo2, ret := device2.GetPciInfo()
		if !errors.Is(ret, nvml.SUCCESS) {
			continue
		}

		// Check if this link connects to device2
		if remoteInfo.Domain == pciInfo2.Domain &&
			remoteInfo.Bus == pciInfo2.Bus &&
			remoteInfo.Device == pciInfo2.Device {
			count++
		}
	}

	if count > 0 {
		return fmt.Sprintf("NV%d", count)
	}
	return ""
}

// collectTopologyMetrics collects GPU and NIC topology information
func collectTopologyMetrics(devices []nvml.Device) {
	numGPUs := len(devices)

	// Batch collect GPU information first to minimize NVML calls
	gpuInfos := make([]gpuTopoInfo, numGPUs)

	// Collect all GPU metadata in one pass
	for i, device := range devices {
		uuid, ret := device.GetUUID()
		if !errors.Is(ret, nvml.SUCCESS) {
			log.Printf("Failed to get UUID for GPU %d: %v", i, nvml.ErrorString(ret))
			continue
		}
		gpuInfos[i].uuid = uuid

		pciInfo, ret := device.GetPciInfo()
		if !errors.Is(ret, nvml.SUCCESS) {
			log.Printf("Failed to get PCI info for GPU %s: %v", uuid, nvml.ErrorString(ret))
			continue
		}
		gpuInfos[i].pciBusId = pciBusIdToString(pciInfo.BusIdLegacy)
		gpuInfos[i].pciInfo = pciInfo

		// Get CPU affinity
		// Request up to 1024 CPUs (16 * 64-bit words)
		cpuSet, ret := device.GetCpuAffinityWithinScope(16, nvml.AFFINITY_SCOPE_NODE)
		if errors.Is(ret, nvml.SUCCESS) {
			gpuInfos[i].cpuAffinity = formatCpuAffinity(cpuSet)
		} else if errors.Is(ret, nvml.ERROR_NOT_SUPPORTED) {
			gpuInfos[i].cpuAffinity = "N/A"
		} else {
			log.Printf("Failed to get CPU affinity for GPU %s: %v", uuid, nvml.ErrorString(ret))
			gpuInfos[i].cpuAffinity = "N/A"
		}

		// Get NUMA affinity (memory affinity)
		// Request up to 1024 nodes (16 * 64-bit words)
		memAffinity, ret := device.GetMemoryAffinity(16, nvml.AFFINITY_SCOPE_NODE)
		if errors.Is(ret, nvml.SUCCESS) {
			gpuInfos[i].numaAffinity = formatCpuAffinity(memAffinity)
		} else if errors.Is(ret, nvml.ERROR_NOT_SUPPORTED) {
			gpuInfos[i].numaAffinity = "N/A"
		} else {
			log.Printf("Failed to get NUMA affinity for GPU %s: %v", uuid, nvml.ErrorString(ret))
			gpuInfos[i].numaAffinity = "N/A"
		}

		// Get GPU NUMA ID
		// For now, mark as N/A since NVML doesn't directly expose this
		// It's typically derived from the memory affinity
		gpuInfos[i].gpuNumaId = "N/A"
	}

	// Collect GPU-to-GPU topology
	for i := 0; i < numGPUs; i++ {
		device1 := devices[i]
		info1 := gpuInfos[i]

		for j := 0; j < numGPUs; j++ {
			device2 := devices[j]
			info2 := gpuInfos[j]

			var connection string
			if i == j {
				connection = "X" // Self
			} else {
				// Check for NVLink connection first
				nvlinkConn := getNVLinkConnection(device1, device2)
				if nvlinkConn != "" {
					connection = nvlinkConn
				} else {
					// Fall back to PCIe topology
					level, ret := device1.GetTopologyCommonAncestor(device2)
					if errors.Is(ret, nvml.SUCCESS) {
						connection = topologyLevelToString(level)
					} else if errors.Is(ret, nvml.ERROR_NOT_SUPPORTED) {
						connection = "UNKNOWN"
					} else {
						log.Printf("Failed to get topology between GPU %s and GPU %s: %v",
							info1.uuid, info2.uuid, nvml.ErrorString(ret))
						continue
					}
				}
			}

			// Set GPU topology metric
			gpuTopology.WithLabelValues(
				info1.uuid,
				info1.pciBusId,
				fmt.Sprintf("GPU%d", i),
				info1.cpuAffinity,
				info1.numaAffinity,
				info1.gpuNumaId,
				"gpu",
				fmt.Sprintf("GPU%d", j),
				connection,
			).Set(1)
		}
	}

	// Collect NIC information
	collectNICTopology(devices, gpuInfos)
}

// gpuTopoInfo stores cached GPU topology information
type gpuTopoInfo struct {
	uuid         string
	pciBusId     string
	cpuAffinity  string
	numaAffinity string
	gpuNumaId    string
	pciInfo      nvml.PciInfo
}

// collectNICTopology collects NIC topology information
func collectNICTopology(gpuDevices []nvml.Device, gpuInfos []gpuTopoInfo) {
	// Get NIC device count
	unitCount, ret := nvml.UnitGetCount()
	if errors.Is(ret, nvml.ERROR_NOT_SUPPORTED) {
		// If units aren't supported, try to enumerate NICs using device API
		// This is a fallback for systems without unit support
		return
	} else if !errors.Is(ret, nvml.SUCCESS) {
		log.Printf("Failed to get unit count: %v", nvml.ErrorString(ret))
		return
	}

	// Enumerate units to find NICs
	nics := make([]nvml.Unit, 0)
	nicNames := make([]string, 0)

	for i := 0; i < unitCount; i++ {
		unit, ret := nvml.UnitGetHandleByIndex(i)
		if !errors.Is(ret, nvml.SUCCESS) {
			continue
		}

		// Get unit info to determine if it's a NIC
		unitInfo, ret := unit.GetUnitInfo()
		if errors.Is(ret, nvml.SUCCESS) {
			// Check if this is a network-related unit
			// Unit types typically include "NIC", "Network", or similar
			nics = append(nics, unit)
			// Convert byte array to string
			name := string(unitInfo.Name[:])
			// Trim null bytes
			if idx := strings.IndexByte(name, 0); idx != -1 {
				name = name[:idx]
			}
			nicNames = append(nicNames, name)
		}
	}

	// For each NIC, get topology to GPUs
	for nicIdx, nic := range nics {
		nicName := nicNames[nicIdx]

		// Get NIC PCI info if available
		nicDevices, ret := nic.GetDevices()
		if !errors.Is(ret, nvml.SUCCESS) {
			log.Printf("Failed to get devices for NIC %s: %v", nicName, nvml.ErrorString(ret))
			continue
		}

		if len(nicDevices) == 0 {
			continue
		}

		// Use first device associated with the NIC to determine topology
		nicDevice := nicDevices[0]

		// Get topology from NIC to each GPU
		for gpuIdx := range gpuInfos {
			if gpuIdx >= len(gpuDevices) {
				break
			}
			gpuDevice := gpuDevices[gpuIdx]
			level, ret := nicDevice.GetTopologyCommonAncestor(gpuDevice)
			if !errors.Is(ret, nvml.SUCCESS) {
				if !errors.Is(ret, nvml.ERROR_NOT_SUPPORTED) {
					log.Printf("Failed to get topology between NIC %s and GPU%d: %v",
						nicName, gpuIdx, nvml.ErrorString(ret))
				}
				continue
			}

			connection := topologyLevelToString(level)

			// Set NIC topology metric for GPU connection
			nicTopology.WithLabelValues(
				nicName,
				fmt.Sprintf("NIC%d", nicIdx),
				"gpu",
				fmt.Sprintf("GPU%d", gpuIdx),
				connection,
			).Set(1)
		}

		// Get topology from NIC to other NICs
		for otherNicIdx, otherNic := range nics {
			if nicIdx == otherNicIdx {
				// Self
				nicTopology.WithLabelValues(
					nicName,
					fmt.Sprintf("NIC%d", nicIdx),
					"nic",
					fmt.Sprintf("NIC%d", otherNicIdx),
					"X",
				).Set(1)
				continue
			}

			otherDevices, ret := otherNic.GetDevices()
			if !errors.Is(ret, nvml.SUCCESS) || len(otherDevices) == 0 {
				continue
			}

			level, ret := nicDevice.GetTopologyCommonAncestor(otherDevices[0])
			if !errors.Is(ret, nvml.SUCCESS) {
				if !errors.Is(ret, nvml.ERROR_NOT_SUPPORTED) {
					log.Printf("Failed to get topology between NIC%d and NIC%d: %v",
						nicIdx, otherNicIdx, nvml.ErrorString(ret))
				}
				continue
			}

			connection := topologyLevelToString(level)

			// Set NIC topology metric for NIC-to-NIC connection
			nicTopology.WithLabelValues(
				nicName,
				fmt.Sprintf("NIC%d", nicIdx),
				"nic",
				fmt.Sprintf("NIC%d", otherNicIdx),
				connection,
			).Set(1)
		}
	}
}