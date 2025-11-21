# nvgpu-exporter

![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/mlmon/nvgpu-exporter) [![Go Report Card](https://goreportcard.com/badge/github.com/mlmon/nvgpu-exporter)](https://goreportcard.com/report/github.com/mlmon/nvgpu-exporter) [![codecov](https://codecov.io/gh/mlmon/nvgpu-exporter/graph/badge.svg?token=2T2YXJGEJV)](https://codecov.io/gh/mlmon/nvgpu-exporter)

nvgpu-exporter is a lightweight Prometheus exporter that surfaces detailed NVIDIA
GPU information and health telemetry pulled directly from NVML. The exporter was
built to make it easy to monitor fabric health on Hopper/Blackwell class GPUs,
track NVLink error counters, and capture Xid events without needing a full
DCGM stack.

## Features

- Collects basic inventory about each GPU (UUID, PCI bus ID, InfoROM versions,
  board/serial numbers) via the `nvgpu_gpu_info` metric.
- Exposes exporter build, CUDA, driver, and NVML versions for traceability via
  `nvgpu_exporter_info`.
- Periodically (configurable) samples NVSwitch fabric state, health summaries,
  and incorrect configuration codes.
- Captures NVLink error counters, BER data, and FEC history fields on a
  per-link basis when supported by the GPU generation.
- Subscribes to NVML Xid events and increments a labeled counter whenever a GPU
  reports a fatal error.
- Ships with a privileged Kubernetes DaemonSet manifest for easy cluster-wide
  deployment on GPU nodes.

## Requirements

- NVIDIA driver stack and NVML available on the host. The exporter calls into
  NVML directly and therefore must run with access to `/dev/nvidia*` and the
  system driver libraries.
- Go 1.21+ (Go 1.25 is used in `go.mod`) if you plan to build from source.
- Prometheus or another metrics scraper polling the `/metrics` endpoint that
  listens on port `9400` by default.

## Quick start

```bash
git clone https://github.com/mlmon/nvgpu-exporter
cd nvgpu-exporter
go build -o nvgpu-exporter ./...
sudo ./nvgpu-exporter -addr :9400 -collection-interval 30s
```

If you prefer a container image, use `ghcr.io/mlmon/nvgpu-exporter/nvgpu-exporter:latest`
and run it with the NVIDIA Container Runtime so the container gains access to
the NVML device nodes and driver libraries. The Kubernetes manifest under
`k8s/daemonset.yaml` shows the required privileges, mounts, and tolerations.

## Configuration

| Flag | Default | Description |
|------|---------|-------------|
| `-addr` | `:9400` | HTTP listen address for the Prometheus `/metrics` endpoint. |
| `-collection-interval` | `60s` | How frequently to refresh fabric health and NVLink error metrics. |

The exporter registers event callbacks for Xid errors, so those metrics update as
soon as NVML emits an event regardless of the collection interval. Inventory
metrics are initialized on startup.

## Metrics

A complete list of emitted metrics, including metric type, label sets, and data
source hints, is available in [`docs/metrics.md`](docs/metrics.md). Highlights:

- `nvgpu_exporter_info`: build metadata and driver/CUDA versions.
- `nvgpu_gpu_info`: GPU inventory labels for easy joins in PromQL.
- `nvgpu_fabric_*`: NVSwitch fabric state, status, health summaries, and
  per-field health flags decoded from the NVML health mask.
- `nvgpu_nvlink_errors_total`: per-link GB200 NVLink error counters, BER data,
  and FEC history values when supported by the hardware.
- `clocks_event_duration_cumulative_total`: cumulative time GPUs spent
  throttled for each NVML clock event reason.
- `nvgpu_gpu_topology_info` / `nvgpu_nic_topology_info`: GPU-to-GPU/CPU/NIC
  relationships derived from NVML topology calls (similar to `nvidia-smi topo -m`).
- `nvgpu_xid_errors_total`: cumulative count of NVML Xid errors by code.

## Scaling guidance

The exporter is lightweight, but each additional feature increases the metric
cardinality per host. As a rough guide for a Hopper/Blackwell system with four
GPUs (HV/HGX B200):

- `nvgpu_gpu_info`: 4 time-series.
- Fabric health/state/status/summary/incorrect configuration: 4 GPUs × ~15
  label combinations ≈ 60 series.
- NVLink errors: GB200 exposes up to 18 links per GPU. With ~30 counters per
  link that’s ~2,160 series per host.
- Clock-event durations: 4 GPUs × 5 reasons = 20 series.
- Topology info: one series per GPU plus a NIC placeholder (~5 series).
- Xid counters: sparse, but plan for a few dozen series depending on error
  variety.

Overall, expect roughly 2.5–3k time-series per GB200 host when all collectors
are enabled. Scale Prometheus accordingly (scrape interval, retention, remote
write, etc.), and consider disabling NVLink field collection or reducing the
frequency if you are monitoring hundreds of nodes.

## Kubernetes deployment

The manifest in `k8s/daemonset.yaml` deploys the exporter as a privileged
DaemonSet on GPU nodes. It already sets the required NVIDIA runtime class,
privileged security context, device mounts, and tolerations. Adjust the
namespace, image tag, or Prometheus scrape annotations as needed in your
cluster.

## Development

1. Ensure Go is installed and `nvml.h`/driver libraries are available locally.
2. Run `go test ./...` (no tests are currently included, but this primes module
   downloads).
3. Build with `go build ./...` and run the exporter on a GPU-capable machine.

The project uses Go modules and has no other build dependencies. Contributions
are welcome—please keep newly added features documented in
[`docs/metrics.md`](docs/metrics.md) so users know how to consume the data.
