package main

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// GpuInfo captures immutable metadata about a GPU returned by NVML.
type GpuInfo struct {
	UUID                string
	PciBusId            string
	PciDomain           uint32
	PciBus              uint32
	PciDevice           uint32
	Name                string
	Brand               string
	Serial              string
	BoardId             string
	OemInforomVersion   string
	EccInforomVersion   string
	PowerInforomVersion string
	VbiosVersion        string
	InforomImageVersion string
	IbGuid              string
	// Platform Info fields
	ChassisSerialNumber string
	SlotNumber          string
	TrayIndex           string
	HostId              string
	PeerType            string
	ModuleId            string
	RackGuid            string
	ChassisPhysicalSlot string
	ComputeSlotIndex    string
	NodeIndex           string
	GpuFabricGuid       string
}

// ExporterInfo stores driver/library versions exposed by the exporter.
type ExporterInfo struct {
	CudaVersion   string
	DriverVersion string
	NVMLVersion   string
}

// DeviceLister abstracts GPU/driver metadata collection so it can be mocked in tests.
type DeviceLister interface {
	Count() int
	GpuInfo(i int) (*GpuInfo, error)
	ExporterInfo() (*ExporterInfo, error)
}

func logDeviceList(devices DeviceLister, logger *slog.Logger) {
	logger.Info("discovered GPUs", "count", devices.Count())

	for i := 0; i < devices.Count(); i++ {
		info, err := devices.GpuInfo(i)
		if err != nil {
			logger.Error("failed to get GPU info", "index", i, "err", err)
			continue
		}

		logger.Info("gpu", "index", i, "name", info.Name, "uuid", info.UUID)
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
	[]string{"UUID", "pci_bus_id", "name", "brand", "serial", "board_id", "vbios_version", "oem_inforom_version", "ecc_inforom_version", "power_inforom_version", "inforom_image_version", "chassis_serial_number", "slot_number", "tray_index", "host_id", "peer_type", "module_id", "gpu_fabric_guid"},
)

func initExporterInfo(devices DeviceLister, version string, commit string) error {
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

func loadGpuInfos(devices DeviceLister) ([]*GpuInfo, error) {
	count := devices.Count()
	infos := make([]*GpuInfo, 0, count)

	for i := 0; i < count; i++ {
		info, err := devices.GpuInfo(i)
		if err != nil {
			return nil, fmt.Errorf("failed to get GPU info for device %d: %w", i, err)
		}
		infos = append(infos, info)
	}

	return infos, nil
}

func initGpuInfoWithCache(infos []*GpuInfo) error {
	for _, info := range infos {

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
			info.ChassisSerialNumber,
			info.SlotNumber,
			info.TrayIndex,
			info.HostId,
			info.PeerType,
			info.ModuleId,
			info.GpuFabricGuid,
		).Set(1)
	}

	// Register the GPU info metric
	prometheus.MustRegister(gpuInfo)

	return nil
}

// startCollectors starts a goroutine that periodically collects fabric health and NVLink error metrics
func startCollectors(devices Devices, interval time.Duration, infos []*GpuInfo, logger *slog.Logger) {
	prometheus.MustRegister(fabricHealth)
	prometheus.MustRegister(fabricState)
	prometheus.MustRegister(fabricStatus)
	prometheus.MustRegister(fabricHealthSummary)
	prometheus.MustRegister(fabricIncorrectConfig)
	prometheus.MustRegister(nvlinkErrors)
	prometheus.MustRegister(clockEventDurations)

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		collectFabricHealth(devices, logger)
		collectNVLinkErrors(devices, logger)
		collectClockEventReasons(devices, logger)

		for range ticker.C {
			collectFabricHealth(devices, logger)
			collectNVLinkErrors(devices, logger)
			collectClockEventReasons(devices, logger)
		}
	}()

	logger.Info("started collectors", "interval", interval)
}
