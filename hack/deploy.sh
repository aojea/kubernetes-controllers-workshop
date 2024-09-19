#!/bin/bash

set -o errexit -o nounset -o pipefail

# For demo
set -x

# cd to the repo root
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." &> /dev/null && pwd -P)"
cd "${REPO_ROOT}"

# Create container image
docker build --load -t aojea/mycontroller:test .

# Load the image in the registry
# We preload this in the kind cluster to shortcut this step
# and avoid the latency of pushing and pulling images
kind load docker-image aojea/mycontroller:test

# Install the deployment
kubectl apply -f ./config/deployment.yaml