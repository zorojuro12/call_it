#!/usr/bin/env bash
# Deletes the local kind cluster and everything in it, volumes included.
set -euo pipefail
kind delete cluster --name callit
