package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/gogunit/gunit/hammy"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestInitExporterInfoRegistersMetric(t *testing.T) {
	assert := hammy.New(t)
	resetExporterInfoMetric(t)

	devices := &stubDeviceLister{
		exporterInfo: &ExporterInfo{
			CudaVersion:   "12.4",
			DriverVersion: "560.35",
			NVMLVersion:   "12.4",
		},
	}

	err := initExporterInfo(devices, "0.2.0", "abcd1234")
	assert.Is(hammy.True(err == nil))

	value := testutil.ToFloat64(exporterInfo.WithLabelValues("0.2.0-abcd1234", "560.35", "12.4", "12.4"))
	assert.Is(hammy.Number(value).EqualTo(1))

	count := testutil.CollectAndCount(exporterInfo)
	assert.Is(hammy.Number(count).EqualTo(1))
}

func TestInitGpuInfoExportsAllDevices(t *testing.T) {
	assert := hammy.New(t)
	resetGpuInfoMetric(t)

	devices := &stubDeviceLister{
		gpuInfos: []*GpuInfo{
			{
				UUID:                "GPU-1",
				PciBusId:            "0000:01:00.0",
				PciDomain:           0,
				PciBus:              1,
				PciDevice:           0,
				Name:                "H100",
				Brand:               "1",
				Serial:              "ABC123",
				BoardId:             "10",
				VbiosVersion:        "95.02",
				OemInforomVersion:   "1.0",
				EccInforomVersion:   "1.0",
				PowerInforomVersion: "1.0",
				InforomImageVersion: "1.0",
				ChassisSerialNumber: "1820425190259",
				SlotNumber:          "15",
				TrayIndex:           "5",
				HostId:              "1",
				PeerType:            "Switch Connected",
				ModuleId:            "2",
				GpuFabricGuid:       "0xec9e1299856d0a6c",
				IbGuid:              "f8c53a7b96de0001",
				RackGuid:            "rack-guid-1",
				ChassisPhysicalSlot: "chassis-slot-1",
				ComputeSlotIndex:    "compute-slot-1",
				NodeIndex:           "node-1",
			},
			{
				UUID:                "GPU-2",
				PciBusId:            "0000:02:00.0",
				PciDomain:           0,
				PciBus:              2,
				PciDevice:           0,
				Name:                "H100",
				Brand:               "1",
				Serial:              "XYZ987",
				BoardId:             "11",
				VbiosVersion:        "95.03",
				OemInforomVersion:   "1.1",
				EccInforomVersion:   "1.1",
				PowerInforomVersion: "1.1",
				InforomImageVersion: "1.1",
				ChassisSerialNumber: "1820425190259",
				SlotNumber:          "16",
				TrayIndex:           "6",
				HostId:              "2",
				PeerType:            "Direct Connected",
				ModuleId:            "3",
				GpuFabricGuid:       "0xec9e1299856d0a6d",
				IbGuid:              "f8c53a7b96de0002",
				RackGuid:            "rack-guid-2",
				ChassisPhysicalSlot: "chassis-slot-2",
				ComputeSlotIndex:    "compute-slot-2",
				NodeIndex:           "node-2",
			},
		},
	}

	infos, err := loadGpuInfos(devices)
	assert.Is(hammy.True(err == nil))

	err = initGpuInfoWithCache(infos)
	assert.Is(hammy.True(err == nil))

	for _, info := range devices.gpuInfos {
		value := testutil.ToFloat64(gpuInfo.WithLabelValues(
			info.UUID,
			info.PciBusId,
			fmt.Sprintf("%d", info.PciDomain),
			fmt.Sprintf("%d", info.PciBus),
			fmt.Sprintf("%d", info.PciDevice),
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
			info.IbGuid,
			info.RackGuid,
			info.ChassisPhysicalSlot,
			info.ComputeSlotIndex,
			info.NodeIndex,
		))
		assert.Is(hammy.Number(value).EqualTo(1))
	}

	count := testutil.CollectAndCount(gpuInfo)
	assert.Is(hammy.Number(count).EqualTo(2))
}

func TestInitGpuInfoPropagatesErrors(t *testing.T) {
	assert := hammy.New(t)
	resetGpuInfoMetric(t)

	devices := &stubDeviceLister{
		gpuInfos: []*GpuInfo{{UUID: "only"}},
		gpuErr:   errors.New("boom"),
	}

	_, err := loadGpuInfos(devices)
	assert.Is(hammy.True(err != nil))
	assert.Is(hammy.String(err.Error()).Contains("failed to get GPU info"))
}

type stubDeviceLister struct {
	exporterInfo *ExporterInfo
	exporterErr  error
	gpuInfos     []*GpuInfo
	gpuErr       error
}

func (s *stubDeviceLister) Count() int {
	return len(s.gpuInfos)
}

func (s *stubDeviceLister) GpuInfo(i int) (*GpuInfo, error) {
	if s.gpuErr != nil {
		return nil, s.gpuErr
	}
	if i < 0 || i >= len(s.gpuInfos) {
		return nil, fmt.Errorf("index %d is out of range", i)
	}
	return s.gpuInfos[i], nil
}

func (s *stubDeviceLister) ExporterInfo() (*ExporterInfo, error) {
	if s.exporterErr != nil {
		return nil, s.exporterErr
	}
	if s.exporterInfo == nil {
		return nil, errors.New("no exporter info")
	}
	return s.exporterInfo, nil
}

func resetExporterInfoMetric(t *testing.T) {
	t.Helper()
	exporterInfo.Reset()
	prometheus.Unregister(exporterInfo)
	t.Cleanup(func() {
		exporterInfo.Reset()
		prometheus.Unregister(exporterInfo)
	})
}

func resetGpuInfoMetric(t *testing.T) {
	t.Helper()
	gpuInfo.Reset()
	prometheus.Unregister(gpuInfo)
	t.Cleanup(func() {
		gpuInfo.Reset()
		prometheus.Unregister(gpuInfo)
	})
}
