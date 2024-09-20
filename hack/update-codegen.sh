#!/usr/bin/env bash

# Copyright 2017 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

readonly SCRIPT_ROOT=$(cd $(dirname ${BASH_SOURCE})/.. && pwd)
echo "SCRIPT_ROOT ${SCRIPT_ROOT}"
cd ${SCRIPT_ROOT}

(
  # To support running this script from anywhere, first cd into this directory,
  # and then install with forced module mode on and fully qualified name.
  cd "$(dirname "${0}")"
  go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.15.0
)


echo "Generating CRD artifacts"
controller-gen rbac:roleName=mycontroller crd \
  object:headerFile="${SCRIPT_ROOT}/hack/boilerplate.go.txt" \
  paths="${SCRIPT_ROOT}/apis/..." \
  output:crd:dir="${SCRIPT_ROOT}/apis"

# https://raw.githubusercontent.com/kubernetes/code-generator/release-1.30/kube_codegen.sh
source "${SCRIPT_ROOT}/hack/kube_codegen.sh"

THIS_PKG="github.com/aojea/kubernetes-controllers-workshop"

kube::codegen::gen_client \
    --with-watch \
    --boilerplate "${SCRIPT_ROOT}/hack/boilerplate.go.txt" \
    --output-dir "${SCRIPT_ROOT}/apis/generated" \
    --output-pkg "${THIS_PKG}/apis/generated" \
    "${SCRIPT_ROOT}"