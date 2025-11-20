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

func initTopologyInfo(devices Devices) error {
	prometheus.MustRegister(gpuTopologyInfo)
	prometheus.MustRegister(nicTopologyInfo)

	if err := collectGpuTopologyInfo(devices); err != nil {
		return err
	}
	collectNicTopologyInfo()
	return nil
}

func collectGpuTopologyInfo(devices Devices) error {
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

	for i := range devices {
		info, err := devices.GpuInfo(i)
		if err != nil {
			return err
		}

		labels := prometheus.Labels{
			"UUID": info.UUID,
			"name": info.Name,
		}

		for idx, key := range gpuLabelKeys {
			if idx >= len(devices) {
				labels[key] = "absent"
				continue
			}

			labels[key] = getTopologyLabel(i, idx)
		}

		for _, key := range nicLabelKeys {
			labels[key] = "unknown"
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

	return nil
}

func collectNicTopologyInfo() {
	labels := prometheus.Labels{"name": "unsupported"}
	for _, key := range append(gpuLabelKeys, nicLabelKeys...) {
		labels[key] = "unknown"
	}
	nicTopologyInfo.With(labels).Set(0)
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
