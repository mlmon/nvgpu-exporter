package main

import (
	"errors"
	"log/slog"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	clockEventDurations = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "clocks_event_duration_cumulative_total",
			Help:      "Accumulated time (nanoseconds) spent throttled per NVML clock event reason.",
		},
		[]string{"UUID", "pci_bus_id", "reason"},
	)

	clockEventReasonFields = []struct {
		fieldID uint32
		reason  string
	}{
		{fieldID: nvml.FI_DEV_CLOCKS_EVENT_REASON_SW_POWER_CAP, reason: "sw_power_capping"},
		{fieldID: nvml.FI_DEV_CLOCKS_EVENT_REASON_SYNC_BOOST, reason: "sync_boost"},
		{fieldID: nvml.FI_DEV_CLOCKS_EVENT_REASON_SW_THERM_SLOWDOWN, reason: "sw_thermal_slowdown"},
		{fieldID: nvml.FI_DEV_CLOCKS_EVENT_REASON_HW_THERM_SLOWDOWN, reason: "hw_thermal_slowdown"},
		{fieldID: nvml.FI_DEV_CLOCKS_EVENT_REASON_HW_POWER_BRAKE_SLOWDOWN, reason: "hw_power_braking"},
	}
)

func collectClockEventReasons(devices []nvml.Device, logger *slog.Logger) {
	for _, device := range devices {
		uuid, ret := device.GetUUID()
		if !errors.Is(ret, nvml.SUCCESS) {
			logger.Warn("failed to get UUID for device", "error", nvml.ErrorString(ret))
			continue
		}

		pciInfo, ret := device.GetPciInfo()
		if !errors.Is(ret, nvml.SUCCESS) {
			logger.Warn("failed to get PCI info", "uuid", uuid, "error", nvml.ErrorString(ret))
			continue
		}
		pciBusId := pciBusIdToString(pciInfo.BusIdLegacy)

		fieldValues, index := buildClockEventRequests()

		ret = device.GetFieldValues(fieldValues)
		if !errors.Is(ret, nvml.SUCCESS) {
			if !errors.Is(ret, nvml.ERROR_NOT_SUPPORTED) {
				logger.Warn("failed to get clock event fields", "uuid", uuid, "error", nvml.ErrorString(ret))
			}
			continue
		}

		for _, field := range clockEventReasonFields {
			fv := fieldValues[index[field.fieldID]]
			if !errors.Is(nvml.Return(fv.NvmlReturn), nvml.SUCCESS) {
				if !errors.Is(nvml.Return(fv.NvmlReturn), nvml.ERROR_NOT_SUPPORTED) {
					logger.Warn("clock event field unavailable", "reason", field.reason, "uuid", uuid, "error", nvml.ErrorString(nvml.Return(fv.NvmlReturn)))
				}
				continue
			}

			durationNanoseconds, err := clockEventFieldValueToNanoseconds(fv)
			if err != nil {
				logger.Warn("failed to decode clock event field", "reason", field.reason, "uuid", uuid, "error", err)
				continue
			}

			clockEventDurations.WithLabelValues(
				uuid,
				pciBusId,
				field.reason,
			).Set(durationNanoseconds)
		}
	}
}

func clockEventFieldValueToNanoseconds(fv nvml.FieldValue) (float64, error) {
	value, err := fieldValueToFloat64(fv)
	if err != nil {
		return 0, err
	}
	return value, nil
}

func buildClockEventRequests() ([]nvml.FieldValue, map[uint32]int) {
	values := make([]nvml.FieldValue, 0, len(clockEventReasonFields))
	index := make(map[uint32]int, len(clockEventReasonFields))

	for _, field := range clockEventReasonFields {
		index[field.fieldID] = len(values)
		values = append(values, nvml.FieldValue{
			FieldId: field.fieldID,
		})
	}

	return values, index
}
