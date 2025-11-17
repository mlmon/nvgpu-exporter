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
	_ "go.uber.org/automaxprocs"
)

const (
	namespace = "nvgpu"
)

var (
	commit  = "unknown"
	version = "0.1.0"

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
		[]string{"UUID", "pci_bus_id", "name", "brand", "serial", "board_id", "vbios_version", "oem_inforom_version", "ecc_inforom_version", "power_inforom_version", "inforom_image_version"},
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

type Devicer interface {
	GetGpuInfo(i int) (*GpuInfo, error)
}

type Devices []nvml.Device

func (d Devices) GetGpuInfo(i int) (*GpuInfo, error) {
	info := &GpuInfo{}
	device := d[i]

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

func initExporterInfo() error {
	// Get driver version
	driverVersion, ret := nvml.SystemGetDriverVersion()
	if !errors.Is(ret, nvml.SUCCESS) {
		return fmt.Errorf("failed to get driver version: %v", nvml.ErrorString(ret))
	}

	// Get NVML version
	nvmlVersion, ret := nvml.SystemGetNVMLVersion()
	if !errors.Is(ret, nvml.SUCCESS) {
		return fmt.Errorf("failed to get NVML version: %v", nvml.ErrorString(ret))
	}

	// Get CUDA version
	cudaVersion, ret := nvml.SystemGetCudaDriverVersion()
	if !errors.Is(ret, nvml.SUCCESS) {
		return fmt.Errorf("failed to get CUDA version: %v", nvml.ErrorString(ret))
	}
	cudaVersionStr := fmt.Sprintf("%d.%d", cudaVersion/1000, (cudaVersion%1000)/10)

	// Set the exporter info metric
	exporterInfo.WithLabelValues(version+"-"+commit, driverVersion, nvmlVersion, cudaVersionStr).Set(1)

	// Register the exporter info metric
	prometheus.MustRegister(exporterInfo)
	return nil
}

func initGpuInfo() ([]nvml.Device, error) {
	// Get device count and populate GPU info metrics
	count, ret := nvml.DeviceGetCount()
	if !errors.Is(ret, nvml.SUCCESS) {
		return nil, fmt.Errorf("failed to get device count: %v", nvml.ErrorString(ret))
	}

	var devices Devices

	for i := 0; i < count; i++ {
		device, ret := nvml.DeviceGetHandleByIndex(i)
		if !errors.Is(ret, nvml.SUCCESS) {
			return nil, fmt.Errorf("failed to get device at index %d: %v", i, nvml.ErrorString(ret))
		}
		devices = append(devices, device)

		info, err := devices.GetGpuInfo(i)
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

// startCollectors starts a goroutine that periodically collects fabric health and NVLink error metrics
func startCollectors(devices []nvml.Device, interval time.Duration) {
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

func main() {
	addr := flag.String("addr", ":9400", "HTTP server address")
	collectionInterval := flag.Duration("collection-interval", 60*time.Second, "Interval for collecting GPU fabric health metrics")
	flag.Parse()

	err := Run(addr, collectionInterval)
	if err != nil {
		log.Fatal(err)
	}
}

func Run(addr *string, collectionInterval *time.Duration) error {
	log.Printf("Starting fabric health collector %v-%v\n", version, commit)

	ret := nvml.Init()
	if !errors.Is(ret, nvml.SUCCESS) {
		return fmt.Errorf("init of NVML failed: %v", nvml.ErrorString(ret))
	}

	defer func() {
		ret := nvml.Shutdown()
		if !errors.Is(ret, nvml.SUCCESS) {
			log.Fatalf("Failed to shutdown NVML: %v", nvml.ErrorString(ret))
		}
	}()

	err := initExporterInfo()
	if err != nil {

		return fmt.Errorf("failed to initialize exporter metrics: %w", err)
	}

	devices, err := initGpuInfo()
	if err != nil {

		return fmt.Errorf("failed to initialize gpu metrics: %w", err)
	}

	// Start fabric health collector
	startCollectors(devices, *collectionInterval)

	// Start Xid event collector
	if err := startXidEventCollector(devices); err != nil {
		return fmt.Errorf("failed to start xid event collector: %w", err)
	}

	listDevices()

	http.Handle("/metrics", promhttp.Handler())

	log.Printf("Starting server on %s", *addr)
	if err := http.ListenAndServe(*addr, nil); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	return nil
}
