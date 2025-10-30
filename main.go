package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	namespace = "nvgpu"
)

var (
	commit             = "unknown"
	version            = "0.1.0"
	addr               = flag.String("addr", ":9400", "HTTP server address")
	collectionInterval = flag.Duration("collection-interval", 60*time.Second, "Interval for collecting GPU fabric health metrics")

	exporterInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "exporter_info",
			Help:      "Information about the nvgpu-exporter.",
		},
		[]string{"version", "driver_version", "nvml_version", "cuda_version"},
	)

	gpuInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "gpu_info",
			Help:      "GPU device information.",
		},
		[]string{"uuid", "pci_bus_id", "name", "brand", "serial", "board_id", "vbios_version", "oem_inforom_version", "ecc_inforom_version", "power_inforom_version", "inforom_image_version"},
	)

	fabricHealth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "fabric_health",
			Help:      "GPU fabric health status (1 = healthy/false, 0 = unhealthy/true).",
		},
		[]string{"uuid", "pci_bus_id", "health_field"},
	)
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

func getGpuVersionDetail(device nvml.Device) (*GpuInfo, error) {
	info := &GpuInfo{}

	// Get UUID
	uuid, ret := device.GetUUID()
	if !errors.Is(ret, nvml.SUCCESS) {
		return nil, fmt.Errorf("failed to get UUID: %v", nvml.ErrorString(ret))
	}
	info.UUID = uuid

	// Get PCI bus ID
	pciInfo, ret := device.GetPciInfo()
	if !errors.Is(ret, nvml.SUCCESS) {
		return nil, fmt.Errorf("failed to get PCI info: %v", nvml.ErrorString(ret))
	}
	info.PciBusId = pciBusIdToString(pciInfo.BusIdLegacy)

	// Get name
	name, ret := device.GetName()
	if !errors.Is(ret, nvml.SUCCESS) {
		return nil, fmt.Errorf("failed to get name: %v", nvml.ErrorString(ret))
	}
	info.Name = name

	// Get brand
	brand, ret := device.GetBrand()
	if !errors.Is(ret, nvml.SUCCESS) {
		return nil, fmt.Errorf("failed to get brand: %v", nvml.ErrorString(ret))
	}
	info.Brand = fmt.Sprintf("%d", brand)

	// Get serial
	serial, ret := device.GetSerial()
	if !errors.Is(ret, nvml.SUCCESS) {
		return nil, fmt.Errorf("failed to get serial: %v", nvml.ErrorString(ret))
	}
	info.Serial = serial

	// Get board ID
	boardId, ret := device.GetBoardId()
	if errors.Is(ret, nvml.ERROR_NOT_SUPPORTED) {
		info.BoardId = "unknown"
	} else if !errors.Is(ret, nvml.SUCCESS) {
		return nil, fmt.Errorf("failed to get board ID: %v", nvml.ErrorString(ret))
	} else {
		info.BoardId = fmt.Sprintf("%d", boardId)
	}

	// Get VBIOS version
	vbios, ret := device.GetVbiosVersion()
	if !errors.Is(ret, nvml.SUCCESS) {
		return nil, fmt.Errorf("failed to get VBIOS version: %v", nvml.ErrorString(ret))
	}
	info.VbiosVersion = vbios

	// Get InfoROM versions
	oemVersion, ret := device.GetInforomVersion(nvml.INFOROM_OEM)
	if !errors.Is(ret, nvml.SUCCESS) {
		return nil, fmt.Errorf("failed to get OEM InfoROM version: %v", nvml.ErrorString(ret))
	}
	info.OemInforomVersion = oemVersion

	eccVersion, ret := device.GetInforomVersion(nvml.INFOROM_ECC)
	if !errors.Is(ret, nvml.SUCCESS) {
		return nil, fmt.Errorf("failed to get ECC InfoROM version: %v", nvml.ErrorString(ret))
	}
	info.EccInforomVersion = eccVersion

	powerVersion, ret := device.GetInforomVersion(nvml.INFOROM_POWER)
	if errors.Is(ret, nvml.ERROR_NOT_SUPPORTED) {
		log.Println("Power InfoROM not supported on this GPU; skipping power metrics")
	} else if !errors.Is(ret, nvml.SUCCESS) {
		return nil, fmt.Errorf("failed to get Power InfoROM version: %v %v", nvml.ErrorString(ret), ret)
	}
	info.PowerInforomVersion = powerVersion

	// Get InfoROM image version
	imageVersion, ret := device.GetInforomImageVersion()
	if !errors.Is(ret, nvml.SUCCESS) {
		return nil, fmt.Errorf("failed to get InfoROM image version: %v", nvml.ErrorString(ret))
	}
	info.InforomImageVersion = imageVersion

	return info, nil
}

func listDevices() {
	count, ret := nvml.DeviceGetCount()
	if !errors.Is(ret, nvml.SUCCESS) {
		log.Fatalf("Failed to get device count: %v", nvml.ErrorString(ret))
	}

	log.Printf("Found %d GPU device(s)\n", count)

	for i := 0; i < count; i++ {
		device, ret := nvml.DeviceGetHandleByIndex(i)
		if !errors.Is(ret, nvml.SUCCESS) {
			log.Fatalf("Failed to get device at index %d: %v", i, nvml.ErrorString(ret))
		}

		name, ret := device.GetName()
		if !errors.Is(ret, nvml.SUCCESS) {
			log.Fatalf("Failed to get device name: %v", nvml.ErrorString(ret))
		}

		uuid, ret := device.GetUUID()
		if !errors.Is(ret, nvml.SUCCESS) {
			log.Fatalf("Failed to get device UUID: %v", nvml.ErrorString(ret))
		}

		log.Printf("Device %d: %s (UUID: %s)\n", i, name, uuid)
	}
}

func initMetrics() ([]nvml.Device, error) {
	// Get driver version
	driverVersion, ret := nvml.SystemGetDriverVersion()
	if !errors.Is(ret, nvml.SUCCESS) {
		return nil, fmt.Errorf("failed to get driver version: %v", nvml.ErrorString(ret))
	}

	// Get NVML version
	nvmlVersion, ret := nvml.SystemGetNVMLVersion()
	if !errors.Is(ret, nvml.SUCCESS) {
		return nil, fmt.Errorf("failed to get NVML version: %v", nvml.ErrorString(ret))
	}

	// Get CUDA version
	cudaVersion, ret := nvml.SystemGetCudaDriverVersion()
	if !errors.Is(ret, nvml.SUCCESS) {
		return nil, fmt.Errorf("failed to get CUDA version: %v", nvml.ErrorString(ret))
	}
	cudaVersionStr := fmt.Sprintf("%d.%d", cudaVersion/1000, (cudaVersion%1000)/10)

	// Set the exporter info metric
	exporterInfo.WithLabelValues(version+"-"+commit, driverVersion, nvmlVersion, cudaVersionStr).Set(1)

	// Register the exporter info metric
	prometheus.MustRegister(exporterInfo)

	// Get device count and populate GPU info metrics
	count, ret := nvml.DeviceGetCount()
	if !errors.Is(ret, nvml.SUCCESS) {
		return nil, fmt.Errorf("failed to get device count: %v", nvml.ErrorString(ret))
	}

	var devices []nvml.Device

	for i := 0; i < count; i++ {
		device, ret := nvml.DeviceGetHandleByIndex(i)
		if !errors.Is(ret, nvml.SUCCESS) {
			return nil, fmt.Errorf("failed to get device at index %d: %v", i, nvml.ErrorString(ret))
		}
		devices = append(devices, device)

		info, err := getGpuVersionDetail(device)
		if err != nil {
			return nil, fmt.Errorf("failed to get GPU info for device %d: %w", i, err)
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

	return devices, nil
}

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

// pciBusIdToString converts a PCI bus ID byte array to a human-readable string
// Standard PCI address format is: DDDD:BB:DD.F (e.g., 0000:00:1e.0)
// This is typically 12-13 characters long
func pciBusIdToString(busId [16]uint8) string {
	// Standard PCI address is domain:bus:device.function (12-13 chars)
	// Find the end by looking for common PCI address length
	str := string(busId[:])
	// Find the last digit or period in the expected PCI format
	for i := 12; i < len(busId) && i < 14; i++ {
		if busId[i] == 0 || busId[i] < 32 || busId[i] > 126 {
			return str[:i]
		}
	}
	return str[:13]
}

// flagToGauge converts a boolean to a float64 for Prometheus gauges
// true (healthy/false) = 1.0, false (unhealthy/true) = 0.0
func flagToGauge(b bool) float64 {
	if b {
		return 1.0
	}
	return 0.0
}

// startFabricHealthCollector starts a goroutine that periodically collects fabric health and NVLink error metrics
func startFabricHealthCollector(devices []nvml.Device, interval time.Duration) {
	// Register the metrics
	prometheus.MustRegister(fabricHealth)
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

func main() {
	flag.Parse()

	log.Printf("Starting fabric health collector %v-%v\n", version, commit)

	ret := nvml.Init()
	if !errors.Is(ret, nvml.SUCCESS) {
		log.Fatalf("Failed to initialize NVML: %v", nvml.ErrorString(ret))
	}
	defer func() {
		ret := nvml.Shutdown()
		if !errors.Is(ret, nvml.SUCCESS) {
			log.Fatalf("Failed to shutdown NVML: %v", nvml.ErrorString(ret))
		}
	}()

	devices, err := initMetrics()
	if err != nil {
		log.Fatalf("Failed to initialize metrics: %v", err)
	}

	// Start fabric health collector
	startFabricHealthCollector(devices, *collectionInterval)

	listDevices()

	http.Handle("/metrics", promhttp.Handler())

	log.Printf("Starting server on %s", *addr)
	if err := http.ListenAndServe(*addr, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
