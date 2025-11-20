package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

func shutdown() {
	ret := nvml.Shutdown()
	if !errors.Is(ret, nvml.SUCCESS) {
		log.Fatalf("Failed to shutdown NVML: %v", nvml.ErrorString(ret))
	}
}

// New initializes the NVML library, discovers every GPU device, and returns the
// handles alongside a cleanup routine that must be called on shutdown.
func New() (Devices, func(), error) {
	ret := nvml.Init()
	if !errors.Is(ret, nvml.SUCCESS) {
		return nil, nil, fmt.Errorf("failed to init NVML: %v", nvml.ErrorString(ret))
	}

	// Get device count and populate GPU info metrics
	count, ret := nvml.DeviceGetCount()
	if !errors.Is(ret, nvml.SUCCESS) {
		return nil, nil, fmt.Errorf("failed to get device count: %v", nvml.ErrorString(ret))
	}

	var devices Devices

	for i := 0; i < count; i++ {
		device, ret := nvml.DeviceGetHandleByIndex(i)
		if !errors.Is(ret, nvml.SUCCESS) {
			return nil, nil, fmt.Errorf("failed to get device handle: %v", nvml.ErrorString(ret))
		}
		devices = append(devices, device)
	}
	return devices, shutdown, nil
}

// Devices is a thin slice wrapper that provides helper methods for NVML queries.
type Devices []nvml.Device

// Count returns how many GPU handles are tracked in the slice.
func (d Devices) Count() int {
	return len(d)
}

// ExporterInfo queries system-wide NVML state to describe the exporter host.
func (d Devices) ExporterInfo() (*ExporterInfo, error) {
	info := &ExporterInfo{}
	var ret nvml.Return
	// Get driver version
	info.DriverVersion, ret = nvml.SystemGetDriverVersion()
	if !errors.Is(ret, nvml.SUCCESS) {
		return nil, fmt.Errorf("failed to get driver version: %v", nvml.ErrorString(ret))
	}

	// Get NVML version
	info.NVMLVersion, ret = nvml.SystemGetNVMLVersion()
	if !errors.Is(ret, nvml.SUCCESS) {
		return nil, fmt.Errorf("failed to get NVML version: %v", nvml.ErrorString(ret))
	}

	// Get CUDA version
	cudaVersion, ret := nvml.SystemGetCudaDriverVersion()
	if !errors.Is(ret, nvml.SUCCESS) {
		return nil, fmt.Errorf("failed to get CUDA version: %v", nvml.ErrorString(ret))
	}
	info.CudaVersion = fmt.Sprintf("%d.%d", cudaVersion/1000, (cudaVersion%1000)/10)

	return info, nil
}

// GpuInfo populates detailed metadata for the GPU at index i.
func (d Devices) GpuInfo(i int) (*GpuInfo, error) {
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
