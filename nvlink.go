package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math"

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
	nvmlFieldIdNvLinkSymbolErrors             = 221
	nvmlFieldIdNvLinkSymbolBER                = 222
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
		[]string{"UUID", "pci_bus_id", "link", "error_type"},
	)

	nvlinkErrorFields = []struct {
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
		{nvmlFieldIdNvLinkSymbolErrors, "symbol_errors"},
	}

	nvlinkBerFields = []struct {
		fieldId int
		name    string
	}{
		{nvmlFieldIdNvLinkEffectiveBER, "effective_ber"},
		{nvmlFieldIdNvLinkSymbolBER, "symbol_ber"},
	}

	nvlinkFecFields = []struct {
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
)

// collectNVLinkErrors collects NVLink error counters for all devices using Field Values API (GB200 compatible)
func collectNVLinkErrors(devices []nvml.Device, logger *slog.Logger) {
	for _, device := range devices {
		uuid, ret := device.GetUUID()
		if !errors.Is(ret, nvml.SUCCESS) {
			logger.Warn("failed to get UUID for device", "error", nvml.ErrorString(ret))
			continue
		}

		// Get PCI bus ID
		pciInfo, ret := device.GetPciInfo()
		if !errors.Is(ret, nvml.SUCCESS) {
			logger.Warn("failed to get PCI info", "uuid", uuid, "error", nvml.ErrorString(ret))
			continue
		}
		pciBusId := pciBusIdToString(pciInfo.BusIdLegacy)

		fieldValues, index := buildDeviceWideNvLinkRequests(device)
		if len(fieldValues) == 0 {
			continue
		}

		ret = device.GetFieldValues(fieldValues)
		if !errors.Is(ret, nvml.SUCCESS) {
			if !errors.Is(ret, nvml.ERROR_NOT_SUPPORTED) {
				logger.Warn("failed to read NVLink field values", "uuid", uuid, "error", nvml.ErrorString(ret))
			}
			continue
		}

		for link := 0; link < nvml.NVLINK_MAX_LINKS; link++ {
			if !linkActive(device, uuid, link, logger) {
				continue
			}

			for _, field := range nvlinkErrorFields {
				fv := fieldValues[index[nvlinkFieldKey{fieldId: field.fieldId, link: link}]]
				if !errors.Is(nvml.Return(fv.NvmlReturn), nvml.SUCCESS) {
					if !errors.Is(nvml.Return(fv.NvmlReturn), nvml.ERROR_NOT_SUPPORTED) {
						logger.Warn("NVLink field not available", "field", field.name, "uuid", uuid, "link", link, "error", nvml.ErrorString(nvml.Return(fv.NvmlReturn)))
					}
					continue
				}

				if f, err := fieldValueToFloat64(fv); err == nil {
					nvlinkErrors.WithLabelValues(
						uuid,
						pciBusId,
						fmt.Sprintf("%d", link),
						field.name,
					).Set(f)
				}
			}

			// Collect BER (Bit Error Rate) metrics
			for _, field := range nvlinkBerFields {
				fv := fieldValues[index[nvlinkFieldKey{fieldId: field.fieldId, link: link}]]
				if !errors.Is(nvml.Return(fv.NvmlReturn), nvml.SUCCESS) {
					if !errors.Is(nvml.Return(fv.NvmlReturn), nvml.ERROR_NOT_SUPPORTED) {
						logger.Warn("BER field not available", "field", field.name, "uuid", uuid, "link", link, "error", nvml.ErrorString(nvml.Return(fv.NvmlReturn)))
					}
					continue
				}

				if berValue, err := decodeBER(fv); err == nil {
					nvlinkErrors.WithLabelValues(
						uuid,
						pciBusId,
						fmt.Sprintf("%d", link),
						field.name,
					).Set(berValue)
				}
			}

			// Collect FEC error history counters
			for _, field := range nvlinkFecFields {
				fv := fieldValues[index[nvlinkFieldKey{fieldId: field.fieldId, link: link}]]
				if !errors.Is(nvml.Return(fv.NvmlReturn), nvml.SUCCESS) {
					if !errors.Is(nvml.Return(fv.NvmlReturn), nvml.ERROR_NOT_SUPPORTED) {
						logger.Warn("FEC field not available", "field", field.name, "uuid", uuid, "link", link, "error", nvml.ErrorString(nvml.Return(fv.NvmlReturn)))
					}
					continue
				}

				if f, err := fieldValueToFloat64(fv); err == nil {
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

type nvlinkFieldKey struct {
	fieldId int
	link    int
}

func linkActive(device nvml.Device, uuid string, link int, logger *slog.Logger) bool {
	state, ret := device.GetNvLinkState(link)
	if !errors.Is(ret, nvml.SUCCESS) {
		if !errors.Is(ret, nvml.ERROR_NOT_SUPPORTED) && !errors.Is(ret, nvml.ERROR_INVALID_ARGUMENT) {
			logger.Warn("failed to get NVLink state", "uuid", uuid, "link", link, "error", nvml.ErrorString(ret))
		}
		return false
	}

	if state != nvml.FEATURE_ENABLED {
		logger.Debug("NVLink state not enabled", "uuid", uuid, "link", link)
		return false
	}

	return true
}

func buildDeviceWideNvLinkRequests(device nvml.Device) ([]nvml.FieldValue, map[nvlinkFieldKey]int) {
	totalFields := len(nvlinkErrorFields) + len(nvlinkBerFields) + len(nvlinkFecFields)
	values := make([]nvml.FieldValue, 0, totalFields*nvml.NVLINK_MAX_LINKS)
	index := make(map[nvlinkFieldKey]int, totalFields*nvml.NVLINK_MAX_LINKS)

	for link := 0; link < nvml.NVLINK_MAX_LINKS; link++ {
		state, ret := device.GetNvLinkState(link)
		if !errors.Is(ret, nvml.SUCCESS) || state != nvml.FEATURE_ENABLED {
			continue
		}

		add := func(fieldID int) {
			key := nvlinkFieldKey{fieldId: fieldID, link: link}
			index[key] = len(values)
			values = append(values, nvml.FieldValue{
				FieldId: uint32(fieldID),
				ScopeId: uint32(link),
			})
		}

		for _, field := range nvlinkErrorFields {
			add(field.fieldId)
		}
		for _, field := range nvlinkBerFields {
			add(field.fieldId)
		}
		for _, field := range nvlinkFecFields {
			add(field.fieldId)
		}
	}

	return values, index
}

// decodeBER decodes a BER (Bit Error Rate) value from NVML FieldValue
// BER is encoded as: mantissa (bits 8-11) and exponent (bits 0-7)
// BER = mantissa × 10^(-exponent)
func decodeBER(fv nvml.FieldValue) (float64, error) {
	// First extract the raw value as uint64
	rawValue, err := fieldValueToUint64(fv)
	if err != nil {
		return 0, err
	}

	// Extract exponent (bits 0-7)
	exponent := rawValue & 0xFF

	// Extract mantissa (bits 8-11) - only 4 bits
	mantissa := (rawValue >> 8) & 0xF

	// Calculate BER: mantissa × 10^(-exponent)
	if exponent == 0 && mantissa == 0 {
		return 0, nil
	}

	berValue := float64(mantissa) * math.Pow10(-int(exponent))
	return berValue, nil
}

// fieldValueToUint64 extracts uint64 from nvml.FieldValue
func fieldValueToUint64(fv nvml.FieldValue) (uint64, error) {
	buf := bytes.NewReader(fv.Value[:])

	switch nvml.ValueType(fv.ValueType) {
	case nvml.VALUE_TYPE_UNSIGNED_INT:
		var v uint32
		if err := binary.Read(buf, binary.LittleEndian, &v); err != nil {
			return 0, err
		}
		return uint64(v), nil

	case nvml.VALUE_TYPE_UNSIGNED_LONG, nvml.VALUE_TYPE_UNSIGNED_LONG_LONG:
		var v uint64
		if err := binary.Read(buf, binary.LittleEndian, &v); err != nil {
			return 0, err
		}
		return v, nil

	default:
		return 0, fmt.Errorf("unsupported field type for BER: %d", fv.ValueType)
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
