#!/bin/bash -eu

kubectl get pods -n monitoring -l app.kubernetes.io/name=nvgpu-exporter -w
