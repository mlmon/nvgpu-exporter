package main

import (
	"flag"
	"log"
	"time"

	_ "go.uber.org/automaxprocs"
)

const (
	namespace = "nvgpu"
)

var (
	commit  = "unknown"
	version = "0.1.0"
)

func main() {
	addr := flag.String("addr", ":9400", "HTTP server address")
	collectionInterval := flag.Duration("collection-interval", 60*time.Second, "Interval for collecting GPU fabric health metrics")
	flag.Parse()

	devices, shutdown, err := New()
	if err != nil {
		log.Fatal(err)
	}
	defer shutdown()

	err = Run(addr, collectionInterval, devices)
	if err != nil {
		log.Fatal(err)
	}
}
