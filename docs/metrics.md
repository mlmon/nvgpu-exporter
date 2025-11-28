# Metrics

All metrics use the `nvgpu_` namespace and are exported via the `/metrics`
endpoint. Gauges whose labels end with `_info` or `*_info` expose inventory data
and are set to `1`. Use the labels to join against other metrics in Prometheus.

- Inventory metrics (`*_info`) are emitted once at startup.
- Fabric and NVLink collectors refresh on the configured collection interval.
- Xid counters are event-driven: they increment as soon as NVML publishes an
  event, independent of the collection loop.

| Metric | Type | Labels | Notes |
|--------|------|--------|-------|
| `nvgpu_exporter_info` | Gauge | `version`, `driver_version`, `nvml_version`, `cuda_version` | Metadata about the running exporter and detected driver stack. |
| `nvgpu_gpu_info` | Gauge | `UUID`, `pci_bus_id`, `name`, `brand`, `serial`, `board_id`, `vbios_version`, `oem_inforom_version`, `ecc_inforom_version`, `power_inforom_version`, `inforom_image_version` | Static GPU inventory attributes populated once on startup. |
| `nvgpu_fabric_health` | Gauge | `UUID`, `pci_bus_id`, `clique_id`, `cluster_uuid`, `health_field` | Per-field fabric health flags decoded from the NVML health mask (`1` = healthy, `0` = unhealthy). |
| `nvgpu_fabric_state` | Gauge | `UUID`, `pci_bus_id`, `clique_id`, `cluster_uuid` | Raw NVML fabric state enum (0 = not supported, 1 = not started, 2 = in progress, 3 = completed). |
| `nvgpu_fabric_status` | Gauge | `UUID`, `pci_bus_id`, `clique_id`, `cluster_uuid` | NVML fabric status code reported by the device. |
| `nvgpu_fabric_health_summary` | Gauge | `UUID`, `pci_bus_id`, `clique_id`, `cluster_uuid` | Collapsed health summary derived in code (0 = not supported, 1 = healthy, 2 = unhealthy, 3 = limited capacity). |
| `nvgpu_fabric_incorrect_configuration` | Gauge | `UUID`, `pci_bus_id`, `clique_id`, `cluster_uuid` | Incorrect configuration bits extracted from the health mask (0 = not supported, 1 = none, other values follow NVML docs). |
| `nvgpu_nvlink_errors_total` | Gauge | `UUID`, `pci_bus_id`, `link`, `error_type` | GB200 NVLink counters per link, covering malformed packets, buffer overruns, BER values, and 16 FEC history buckets. |
| `nvgpu_clocks_event_duration_nanoseconds_total` | Gauge | `UUID`, `pci_bus_id`, `reason` | Accumulated throttling time (nanoseconds) for key NVML clock event reasons (SW power capping, Sync Boost, SW/HW thermal, HW power brake). |
| `nvgpu_xid_errors_total` | Counter | `UUID`, `pci_bus_id`, `xid` | Total NVML Xid critical errors seen since exporter start. |

## Fabric health fields

`nvgpu_fabric_health` uses the `health_field` label to describe which bit of the
NVML fabric health mask was decoded. Current values:

- `degraded_bandwidth`
- `route_recovery`
- `route_unhealthy`
- `access_timeout_recovery`

The derived summary (`nvgpu_fabric_health_summary`) leverages these fields plus
the incorrect configuration bits to map the NVML-provided enums into a
Prometheus-friendly gauge. A summary of `3` indicates the fabric is up but
running in a reduced-capacity mode (often because of an incorrect topology or
disabled link).

## NVLink error types

`nvgpu_nvlink_errors_total` enumerates a handful of `error_type` values per link:

- `malformed_packet_errors`
- `buffer_overrun_errors`
- `local_link_integrity_errors`
- `recovery_successful_events`
- `recovery_failed_events`
- `recovery_events`
- `effective_errors`
- `symbol_errors`
- `effective_ber` (decoded BER value)
- `symbol_ber` (decoded BER value)
- `fec_errors_0`...`fec_errors_15` (history buckets)

Not all GPUs implement the GB200 field IDs. When a field is unsupported,
no sample is emitted for that `(UUID, link, error_type)` combination.

Consider alerting on positive rates for non-BER counters and using BER values
as an SLO indicator rather than a hard failure signal. BER spikes should
correlate with FEC bucket growth and can precede link failures.

## Xid event handling

`nvgpu_xid_errors_total` increments whenever NVML emits an Xid critical event.
The exporter subscribes to events as soon as it starts, so metrics update close
to real time even if the standard collection loop is configured with a long
interval. Use the numeric `xid` label to join against NVIDIA's public Xid
reference to understand the underlying issue. A sustained increase often means
the GPU needs operator attention or a workload needs to be rescheduled.

## Joining and labeling tips

- Prefer joins on `UUID` rather than `pci_bus_id` when correlating metrics across
  collectors; UUIDs are stable even if slots are rearranged.
- `cluster_uuid` groups fabric participants that share the same NVSwitch domain.
- Use `clique_id` to distinguish GPUs inside the same cluster that are part of
  different NVSwitch cliques.

## Alerting ideas

- Alert when `nvgpu_fabric_health_summary` is `2` (unhealthy) for more than one
  scrape interval.
- Alert on any positive rate of `nvgpu_xid_errors_total` grouped by GPU UUID.
- Track `nvgpu_clocks_event_duration_nanoseconds_total` deltas to find nodes
  spending excessive time throttled by thermal or power events.
