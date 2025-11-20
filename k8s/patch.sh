#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: $0 <image-tag> [namespace]" >&2
  exit 1
fi

image_tag="$1"
namespace="${2:-monitoring}"
daemonset="nvgpu-exporter"
container="nvml"
image="ghcr.io/mlmon/nvgpu-exporter/nvgpu-exporter:${image_tag}"

kubectl -n "${namespace}" patch daemonset "${daemonset}" \
  --type=strategic \
  -p="{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"$container\",\"image\":\"$image\"}]}}}}"

echo "Patched daemonset ${namespace}/${daemonset} to use image ${image}"
