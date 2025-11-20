package main

import (
	"errors"
	"log"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	clockEventDurations = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "clocks_event_duration_seconds_total",
			Help:      "Accumulated time spent throttled per NVML clock event reason.",
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

func collectClockEventReasons(devices []nvml.Device) {
	for _, device := range devices {
		uuid, ret := device.GetUUID()
		if !errors.Is(ret, nvml.SUCCESS) {
			log.Printf("Failed to get UUID for device: %v", nvml.ErrorString(ret))
			continue
		}

		pciInfo, ret := device.GetPciInfo()
		if !errors.Is(ret, nvml.SUCCESS) {
			log.Printf("Failed to get PCI info for device %s: %v", uuid, nvml.ErrorString(ret))
			continue
		}
		pciBusId := pciBusIdToString(pciInfo.BusIdLegacy)

		for _, field := range clockEventReasonFields {
			values := []nvml.FieldValue{
				{
					FieldId: field.fieldID,
				},
			}

			ret := device.GetFieldValues(values)
			if !errors.Is(ret, nvml.SUCCESS) {
				if !errors.Is(ret, nvml.ERROR_NOT_SUPPORTED) {
					log.Printf("Failed to get clock event field %s for device %s: %v", field.reason, uuid, nvml.ErrorString(ret))
				}
				continue
			}

			if len(values) == 0 {
				continue
			}

			durationSeconds, err := clockEventFieldValueToSeconds(values[0])
			if err != nil {
				log.Printf("Failed to decode clock event field %s for device %s: %v", field.reason, uuid, err)
				continue
			}

			clockEventDurations.WithLabelValues(
				uuid,
				pciBusId,
				field.reason,
			).Set(durationSeconds)
		}
	}
}

func clockEventFieldValueToSeconds(fv nvml.FieldValue) (float64, error) {
	value, err := fieldValueToFloat64(fv)
	if err != nil {
		return 0, err
	}
	// NVML reports throttle counters in nanoseconds, convert to seconds.
	return value / 1e9, nil
}
