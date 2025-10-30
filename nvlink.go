package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	// GB200 NVLink Field Value IDs for error counters
	// These are used with DeviceGetFieldValues API
	nvmlFieldIdNvLinkMalformedPacketErrors    = 206
	nvmlFieldIdNvLinkBufferOverrunErrors      = 207
	nvmlFieldIdNvLinkLocalLinkIntegrityErrors = 211
	nvmlFieldIdNvLinkRecoverySuccessfulEvents = 213
	nvmlFieldIdNvLinkRecoveryFailedEvents     = 214
	nvmlFieldIdNvLinkRecoveryEvents           = 215
	nvmlFieldIdNvLinkEffectiveErrors          = 219
	nvmlFieldIdNvLinkEffectiveBER             = 220
	nvmlFieldIdNvLinkFECHistory0              = 235
	nvmlFieldIdNvLinkFECHistory1              = 236
	nvmlFieldIdNvLinkFECHistory2              = 237
	nvmlFieldIdNvLinkFECHistory3              = 238
	nvmlFieldIdNvLinkFECHistory4              = 239
	nvmlFieldIdNvLinkFECHistory5              = 240
	nvmlFieldIdNvLinkFECHistory6              = 241
	nvmlFieldIdNvLinkFECHistory7              = 242
	nvmlFieldIdNvLinkFECHistory8              = 243
	nvmlFieldIdNvLinkFECHistory9              = 244
	nvmlFieldIdNvLinkFECHistory10             = 245
	nvmlFieldIdNvLinkFECHistory11             = 246
	nvmlFieldIdNvLinkFECHistory12             = 247
	nvmlFieldIdNvLinkFECHistory13             = 248
	nvmlFieldIdNvLinkFECHistory14             = 249
	nvmlFieldIdNvLinkFECHistory15             = 250
)

var (
	nvlinkErrors = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "nvlink_errors_total",
			Help:      "Total NVLink errors by type.",
		},
		[]string{"uuid", "pci_bus_id", "link", "error_type"},
	)
)

// collectNVLinkErrors collects NVLink error counters for all devices using Field Values API (GB200 compatible)
func collectNVLinkErrors(devices []nvml.Device) {
	// GB200 NVLink error counter field IDs
	errorFields := []struct {
		fieldId int
		name    string
	}{
		{nvmlFieldIdNvLinkMalformedPacketErrors, "malformed_packet_errors"},
		{nvmlFieldIdNvLinkBufferOverrunErrors, "buffer_overrun_errors"},
		{nvmlFieldIdNvLinkLocalLinkIntegrityErrors, "local_link_integrity_errors"},
		{nvmlFieldIdNvLinkRecoverySuccessfulEvents, "recovery_successful_events"},
		{nvmlFieldIdNvLinkRecoveryFailedEvents, "recovery_failed_events"},
		{nvmlFieldIdNvLinkRecoveryEvents, "recovery_events"},
		{nvmlFieldIdNvLinkEffectiveErrors, "effective_errors"},
		{nvmlFieldIdNvLinkEffectiveBER, "effective_ber_errors"},
	}

	// FEC error history counters (0-15)
	fecFields := []struct {
		fieldId int
		name    string
	}{
		{nvmlFieldIdNvLinkFECHistory0, "fec_errors_0"},
		{nvmlFieldIdNvLinkFECHistory1, "fec_errors_1"},
		{nvmlFieldIdNvLinkFECHistory2, "fec_errors_2"},
		{nvmlFieldIdNvLinkFECHistory3, "fec_errors_3"},
		{nvmlFieldIdNvLinkFECHistory4, "fec_errors_4"},
		{nvmlFieldIdNvLinkFECHistory5, "fec_errors_5"},
		{nvmlFieldIdNvLinkFECHistory6, "fec_errors_6"},
		{nvmlFieldIdNvLinkFECHistory7, "fec_errors_7"},
		{nvmlFieldIdNvLinkFECHistory8, "fec_errors_8"},
		{nvmlFieldIdNvLinkFECHistory9, "fec_errors_9"},
		{nvmlFieldIdNvLinkFECHistory10, "fec_errors_10"},
		{nvmlFieldIdNvLinkFECHistory11, "fec_errors_11"},
		{nvmlFieldIdNvLinkFECHistory12, "fec_errors_12"},
		{nvmlFieldIdNvLinkFECHistory13, "fec_errors_13"},
		{nvmlFieldIdNvLinkFECHistory14, "fec_errors_14"},
		{nvmlFieldIdNvLinkFECHistory15, "fec_errors_15"},
	}

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

		// Iterate through each NVLink
		for link := 0; link < nvml.NVLINK_MAX_LINKS; link++ {
			// Check if link is active
			state, ret := device.GetNvLinkState(link)
			if !errors.Is(ret, nvml.SUCCESS) {
				// Skip this link - likely not available or not supported
				if !errors.Is(ret, nvml.ERROR_NOT_SUPPORTED) && !errors.Is(ret, nvml.ERROR_INVALID_ARGUMENT) {
					log.Printf("Failed to get NVLink state for device %s link %d: %v", uuid, link, nvml.ErrorString(ret))
				}
				continue
			}
			if state != nvml.FEATURE_ENABLED {
				log.Printf("NVLink state for device %s link %d is not enabled", uuid, link)
				continue
			}

			// Collect standard error counters using Field Values API
			for _, field := range errorFields {
				values := []nvml.FieldValue{
					{
						FieldId: uint32(field.fieldId),
						ScopeId: uint32(link),
					},
				}

				ret := device.GetFieldValues(values)
				if !errors.Is(ret, nvml.SUCCESS) {
					// Log unexpected errors, but not "not supported" errors
					if !errors.Is(ret, nvml.ERROR_NOT_SUPPORTED) {
						log.Printf("Failed to get NVLink field %s for device %s link %d: %v",
							field.name, uuid, link, nvml.ErrorString(ret))
					}
					continue
				}

				if len(values) > 0 {
					if f, err := fieldValueToFloat64(values[0]); err == nil {
						nvlinkErrors.WithLabelValues(
							uuid,
							pciBusId,
							fmt.Sprintf("%d", link),
							field.name,
						).Set(f)
					}
				}
			}

			// Collect FEC error history counters
			for _, field := range fecFields {
				values := []nvml.FieldValue{
					{
						FieldId: uint32(field.fieldId),
						ScopeId: uint32(link),
					},
				}

				ret := device.GetFieldValues(values)
				if !errors.Is(ret, nvml.SUCCESS) {
					if !errors.Is(ret, nvml.ERROR_NOT_SUPPORTED) {
						log.Printf("Failed to get NVLink FEC field %s for device %s link %d: %v",
							field.name, uuid, link, nvml.ErrorString(ret))
					}
					continue
				}

				if len(values) > 0 {
					if f, err := fieldValueToFloat64(values[0]); err == nil {
						nvlinkErrors.WithLabelValues(
							uuid,
							pciBusId,
							fmt.Sprintf("%d", link),
							field.name,
						).Set(f)
					}
				}
			}
		}
	}
}

// fieldValueToFloat64 converts nvml.FieldValue to float64
// by decoding the 8-byte Value buffer according to FieldType.
func fieldValueToFloat64(fv nvml.FieldValue) (float64, error) {
	buf := bytes.NewReader(fv.Value[:]) // Value is typically [8]byte

	switch nvml.ValueType(fv.ValueType) {
	case nvml.VALUE_TYPE_DOUBLE:
		var v float64
		if err := binary.Read(buf, binary.LittleEndian, &v); err != nil {
			return 0, err
		}
		return v, nil

	case nvml.VALUE_TYPE_UNSIGNED_INT:
		var v uint32
		if err := binary.Read(buf, binary.LittleEndian, &v); err != nil {
			return 0, err
		}
		return float64(v), nil

	case nvml.VALUE_TYPE_SIGNED_INT:
		var v int32
		if err := binary.Read(buf, binary.LittleEndian, &v); err != nil {
			return 0, err
		}
		return float64(v), nil

	case nvml.VALUE_TYPE_UNSIGNED_LONG, nvml.VALUE_TYPE_UNSIGNED_LONG_LONG:
		// NVML often uses 64-bit for these
		var v uint64
		if err := binary.Read(buf, binary.LittleEndian, &v); err != nil {
			return 0, err
		}
		return float64(v), nil

	default:
		return 0, fmt.Errorf("unsupported field type: %d", fv.ValueType)
	}
}