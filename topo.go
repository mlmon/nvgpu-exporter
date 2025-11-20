package main

import (
	"errors"
	"fmt"
	"math/bits"
	"runtime"
	"strings"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/prometheus/client_golang/prometheus"
)

type nicInfo struct {
	label       string
	pciKey      string
	connections map[int]struct{}
}

var (
	gpuTopologyInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "gpu_topology_info",
			Help:      "Represents GPU-to-GPU/NIC/CPU relationships similar to `nvidia-smi topo -m`.",
		},
		[]string{
			"UUID",
			"name",
			"gpu0", "gpu1", "gpu2", "gpu3",
			"nic0", "nic1", "nic2", "nic3", "nic4", "nic5",
			"cpu_affinity", "numa_affinity", "gpu_numa_id",
		},
	)

	nicTopologyInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "nic_topology_info",
			Help:      "Represents NIC visibility within the topology map (limited NVML support).",
		},
		[]string{
			"name",
			"gpu0", "gpu1", "gpu2", "gpu3",
			"nic0", "nic1", "nic2", "nic3", "nic4", "nic5",
		},
	)

	gpuLabelKeys = []string{"gpu0", "gpu1", "gpu2", "gpu3"}
	nicLabelKeys = []string{"nic0", "nic1", "nic2", "nic3", "nic4", "nic5"}
)

func initTopologyInfo(devices Devices, infos []*GpuInfo) error {
	prometheus.MustRegister(gpuTopologyInfo)
	prometheus.MustRegister(nicTopologyInfo)

	if err := collectGpuTopologyInfo(devices, infos); err != nil {
		return err
	}
	return nil
}

func collectGpuTopologyInfo(devices Devices, infos []*GpuInfo) error {
	numCPUs := runtime.NumCPU()
	type topoKey struct {
		a int
		b int
	}
	topologyCache := make(map[topoKey]string)

	getTopologyLabel := func(a, b int) string {
		if a == b {
			return "X"
		}
		key := topoKey{a: min(a, b), b: max(a, b)}
		if val, ok := topologyCache[key]; ok {
			return val
		}
		level, ret := devices[a].GetTopologyCommonAncestor(devices[b])
		label := "unknown"
		if errors.Is(ret, nvml.SUCCESS) {
			label = topologyLevelToString(level)
		}
		topologyCache[key] = label
		return label
	}

	nicList := discoverNicLinks(devices)

	for i := range devices {
		info, err := devices.GpuInfo(i)
		if err != nil {
			return err
		}

		labels := prometheus.Labels{
			"UUID": info.UUID,
			"name": fmt.Sprintf("gpu%d", i),
		}

		for idx, key := range gpuLabelKeys {
			if idx >= len(devices) {
				labels[key] = "absent"
				continue
			}

			labels[key] = getTopologyLabel(i, idx)
		}

		for idx, key := range nicLabelKeys {
			if idx >= len(nicList) {
				labels[key] = "absent"
				continue
			}
			if _, ok := nicList[idx].connections[i]; ok {
				labels[key] = "NODE"
			} else {
				labels[key] = "SYS"
			}
		}

		labels["cpu_affinity"] = getCpuAffinityString(devices[i], numCPUs)
		numaNode, ret := devices[i].GetNumaNodeId()
		if errors.Is(ret, nvml.SUCCESS) {
			nodeStr := fmt.Sprintf("%d", numaNode)
			labels["numa_affinity"] = "node-" + nodeStr
			labels["gpu_numa_id"] = nodeStr
		} else {
			labels["numa_affinity"] = "unknown"
			labels["gpu_numa_id"] = "unknown"
		}

		gpuTopologyInfo.With(labels).Set(1)
	}

	recordNicTopology(nicList, len(devices))

	return nil
}

func topologyLevelToString(level nvml.GpuTopologyLevel) string {
	switch level {
	case nvml.TOPOLOGY_INTERNAL:
		return "SOC"
	case nvml.TOPOLOGY_SINGLE:
		return "PIX"
	case nvml.TOPOLOGY_MULTIPLE:
		return "PXB"
	case nvml.TOPOLOGY_HOSTBRIDGE:
		return "PHB"
	case nvml.TOPOLOGY_NODE:
		return "NODE"
	case nvml.TOPOLOGY_SYSTEM:
		return "SYS"
	default:
		return "UNKNOWN"
	}
}

func getCpuAffinityString(device nvml.Device, numCPUs int) string {
	mask, ret := device.GetCpuAffinity(numCPUs)
	if !errors.Is(ret, nvml.SUCCESS) || len(mask) == 0 {
		return "unknown"
	}

	var cpus []int
	for idx, word := range mask {
		for bit := 0; bit < bits.UintSize; bit++ {
			if word&(1<<uint(bit)) == 0 {
				continue
			}
			cpu := idx*bits.UintSize + bit
			if cpu >= numCPUs {
				continue
			}
			cpus = append(cpus, cpu)
		}
	}

	if len(cpus) == 0 {
		return "unknown"
	}

	var ranges []string
	start := cpus[0]
	prev := cpus[0]
	for _, cpu := range cpus[1:] {
		if cpu == prev+1 {
			prev = cpu
			continue
		}
		ranges = append(ranges, formatRange(start, prev))
		start = cpu
		prev = cpu
	}
	ranges = append(ranges, formatRange(start, prev))
	return strings.Join(ranges, ",")
}

func formatRange(start, end int) string {
	if start == end {
		return fmt.Sprintf("%d", start)
	}
	return fmt.Sprintf("%d-%d", start, end)
}

func discoverNicLinks(devices Devices) []*nicInfo {
	nicByKey := make(map[string]*nicInfo)
	var nicList []*nicInfo

	for i := range devices {
		for link := 0; link < nvml.NVLINK_MAX_LINKS; link++ {
			state, ret := devices[i].GetNvLinkState(link)
			if !errors.Is(ret, nvml.SUCCESS) || state != nvml.FEATURE_ENABLED {
				continue
			}

			remoteType, ret := devices[i].GetNvLinkRemoteDeviceType(link)
			if !errors.Is(ret, nvml.SUCCESS) || remoteType != nvml.NVLINK_DEVICE_TYPE_IBMNPU {
				continue
			}

			remotePci, ret := devices[i].GetNvLinkRemotePciInfo(link)
			if !errors.Is(ret, nvml.SUCCESS) {
				continue
			}

			pciKey := pciBusIdToString(remotePci.BusIdLegacy)
			nic := nicByKey[pciKey]
			if nic == nil {
				if len(nicList) >= len(nicLabelKeys) {
					continue
				}
				nic = &nicInfo{
					label:       nicLabelKeys[len(nicList)],
					pciKey:      pciKey,
					connections: make(map[int]struct{}),
				}
				nicByKey[pciKey] = nic
				nicList = append(nicList, nic)
			}
			nic.connections[i] = struct{}{}
		}
	}

	return nicList
}

func recordNicTopology(nicList []*nicInfo, gpuCount int) {
	for idx, nic := range nicList {
		labels := prometheus.Labels{
			"name": nic.label,
		}

		for gpuIdx, key := range gpuLabelKeys {
			if gpuIdx >= gpuCount {
				labels[key] = "absent"
				continue
			}
			if _, ok := nic.connections[gpuIdx]; ok {
				labels[key] = "NODE"
			} else {
				labels[key] = "SYS"
			}
		}

		for nicIdx, key := range nicLabelKeys {
			if nicIdx == idx {
				labels[key] = "X"
			} else {
				labels[key] = "unknown"
			}
		}

		nicTopologyInfo.With(labels).Set(1)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
