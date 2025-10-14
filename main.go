package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	addr  = flag.String("addr", ":9400", "HTTP server address")
	debug = flag.Bool("debug", false, "Enable debug output")
)

func listDevices() {
	count, ret := nvml.DeviceGetCount()
	if !errors.Is(ret, nvml.SUCCESS) {
		log.Fatalf("Failed to get device count: %v", nvml.ErrorString(ret))
	}

	fmt.Printf("Found %d GPU device(s)\n", count)

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

		fmt.Printf("Device %d: %s (UUID: %s)\n", i, name, uuid)
	}
}

func main() {
	flag.Parse()

	ret := nvml.Init()
	if ret != nvml.SUCCESS {
		log.Fatalf("Failed to initialize NVML: %v", nvml.ErrorString(ret))
	}
	defer func() {
		ret := nvml.Shutdown()
		if ret != nvml.SUCCESS {
			log.Fatalf("Failed to shutdown NVML: %v", nvml.ErrorString(ret))
		}
	}()

	if *debug {
		listDevices()
	}

	http.Handle("/metrics", promhttp.Handler())

	log.Printf("Starting server on %s", *addr)
	if err := http.ListenAndServe(*addr, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
