package main

import (
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	xidErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "xid_errors_total",
			Help:      "Total count of GPU Xid errors by error code and GPU UUID.",
		},
		[]string{"UUID", "pci_bus_id", "xid"},
	)

	// Concurrent map to track Xid counts per GPU
	xidCounts sync.Map
)

// xidKey is used as a key in the concurrent map
type xidKey struct {
	UUID string
	Xid  uint64
}

// startXidEventCollector starts a goroutine that subscribes to NVML events and collects Xid errors
func startXidEventCollector(devices []nvml.Device) error {
	// Register the Xid errors metric
	prometheus.MustRegister(xidErrors)

	// Create event set
	eventSet, ret := nvml.EventSetCreate()
	if !errors.Is(ret, nvml.SUCCESS) {
		return errors.New("failed to create event set: " + nvml.ErrorString(ret))
	}

	// Register all devices for Xid events
	eventTypes := uint64(nvml.EventTypeXidCriticalError)
	for _, device := range devices {
		ret = nvml.DeviceRegisterEvents(device, eventTypes, eventSet)
		if !errors.Is(ret, nvml.SUCCESS) {
			log.Printf("Warning: failed to register events for device: %v", nvml.ErrorString(ret))
			continue
		}
	}

	// Start event collection goroutine
	go func() {
		log.Println("Started Xid event collector")
		for {
			// Wait for events (timeout in milliseconds)
			event, ret := nvml.EventSetWait(eventSet, 5000)
			if errors.Is(ret, nvml.ERROR_TIMEOUT) {
				// Timeout is normal, just continue waiting
				continue
			}
			if !errors.Is(ret, nvml.SUCCESS) {
				log.Printf("Error waiting for events: %v", nvml.ErrorString(ret))
				continue
			}

			// Process the event if it's an Xid error
			if event.EventType&nvml.EventTypeXidCriticalError != 0 {
				handleXidEvent(event)
			}
		}
	}()

	return nil
}

// handleXidEvent processes a Xid event and increments the appropriate counter
func handleXidEvent(event nvml.EventData) {
	// Get device UUID
	uuid, ret := event.Device.GetUUID()
	if !errors.Is(ret, nvml.SUCCESS) {
		log.Printf("Failed to get UUID for device in Xid event: %v", nvml.ErrorString(ret))
		return
	}

	// Get PCI bus ID
	pciInfo, ret := event.Device.GetPciInfo()
	if !errors.Is(ret, nvml.SUCCESS) {
		log.Printf("Failed to get PCI info for device in Xid event: %v", nvml.ErrorString(ret))
		return
	}
	pciBusId := pciBusIdToString(pciInfo.BusIdLegacy)

	xid := event.EventData

	// Create key for concurrent map
	key := xidKey{
		UUID: uuid,
		Xid:  xid,
	}

	// Increment count in concurrent map
	count := uint64(1)
	if val, ok := xidCounts.LoadOrStore(key, count); ok {
		count = val.(uint64) + 1
		xidCounts.Store(key, count)
	}

	// Increment Prometheus counter
	xidErrors.WithLabelValues(uuid, pciBusId, formatXid(xid)).Inc()

	log.Printf("Xid error detected - UUID: %s, PCI Bus ID: %s, Xid: %d", uuid, pciBusId, xid)
}

// formatXid converts the Xid to a string for use in labels
func formatXid(xid uint64) string {
	return fmt.Sprintf("%d", xid)
}
