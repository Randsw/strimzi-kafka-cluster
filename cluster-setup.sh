#!/usr/bin/env bash

set -euo pipefail

# CONSTANTS

readonly KIND_NODE_IMAGE=kindest/node:v1.30.0
readonly KIND_NETWORK=kind
readonly CLUSTER_NAME=kind
readonly HOSTS_DOMAIN=kind.cluster

# FUNCTIONS

log(){
  echo "---------------------------------------------------------------------------------------"
  echo "$1"
  echo "---------------------------------------------------------------------------------------"
}

wait_ready(){
  local NAME=${1:-pods}
  local TIMEOUT=${2:-5m}
  local SELECTOR=${3:---all}

  log "WAIT $NAME ($TIMEOUT) ..."

  kubectl wait -A --timeout="$TIMEOUT" --for=condition=ready "$NAME" $SELECTOR
}

wait_pods_ready(){
  local TIMEOUT=${1:-5m}

  wait_ready pods $TIMEOUT --field-selector=status.phase!=Succeeded
}

wait_nodes_ready(){
  local TIMEOUT=${1:-5m}

  wait_ready nodes $TIMEOUT
}

network(){
  local NAME=${1:-$KIND_NETWORK}

  log "NETWORK ($NAME) ..."

  if [ -z "$(docker network ls --filter "name=^${NAME}$" --format='{{ .Name }}')" ]
  then
    docker network create "$NAME"
    echo "Network $NAME created"
  else
    echo "Network $NAME already exists, skipping"
  fi
}

proxy(){
  local NAME=$1
  local TARGET=$2

  if [ -z "$(docker ps -a --filter "name=^${NAME}$" --format='{{ .Names }}')" ]
  then
    docker run -d --name "$NAME" --restart=always --net="$KIND_NETWORK" \
      -e REGISTRY_PROXY_REMOTEURL="$TARGET" registry:2
    echo "Proxy $NAME (-> $TARGET) created"
  else
    echo "Proxy $NAME already exists, skipping"
  fi
}

proxies(){
  log "REGISTRY PROXIES ..."

  proxy proxy-docker-hub    https://registry-1.docker.io
  proxy proxy-quay          https://quay.io
  proxy proxy-gcr           https://gcr.io
  proxy proxy-k8s-gcr       https://k8s.gcr.io
  proxy proxy-k8s-registry  https://registry.k8s.io
}

get_service_lb_ip(){
  kubectl get svc -n "$1" "$2" -o jsonpath='{.status.loadBalancer.ingress[0].ip}'
}

get_subnet(){
  docker network inspect -f '{{(index .IPAM.Config 0).Subnet}}' "$1"
}

subnet_to_ip(){
  local SUBNET=$1
  local HOST_PART=$2

  # Strip the trailing "0.0/<prefix>" keeping the /16 network prefix, e.g.
  # 172.18.0.0/16 -> "172.18." so 172.18.0.0/16 + 255.200 = 172.18.255.200
  local PREFIX=${SUBNET%%0.0/*}

  echo "${PREFIX}${HOST_PART}"
}

cluster(){
  local NAME=${1:-$CLUSTER_NAME}

  log "CLUSTER ..."

  docker pull "$KIND_NODE_IMAGE"

  kind create cluster --name "$NAME" --image "$KIND_NODE_IMAGE" --config - <<EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
kubeadmConfigPatches:
  - |-
    kind: ClusterConfiguration
    controllerManager:
      extraArgs:
        bind-address: 0.0.0.0
    etcd:
      local:
        extraArgs:
          listen-metrics-urls: http://0.0.0.0:2381
    scheduler:
      extraArgs:
        bind-address: 0.0.0.0
containerdConfigPatches:
  - |-
    [plugins."io.containerd.grpc.v1.cri".registry.mirrors."docker.io"]
      endpoint = ["http://proxy-docker-hub:5000"]
  - |-
    [plugins."io.containerd.grpc.v1.cri".registry.mirrors."quay.io"]
      endpoint = ["http://proxy-quay:5000"]
  - |-
    [plugins."io.containerd.grpc.v1.cri".registry.mirrors."k8s.gcr.io"]
      endpoint = ["http://proxy-k8s-gcr:5000"]
  - |-
    [plugins."io.containerd.grpc.v1.cri".registry.mirrors."gcr.io"]
      endpoint = ["http://proxy-gcr:5000"]
  - |-
    [plugins."io.containerd.grpc.v1.cri".registry.mirrors."registry.k8s.io"]
      endpoint = ["http://proxy-k8s-registry:5000"]
nodes:
  - role: control-plane
  - role: worker
  - role: worker
  - role: worker
  - role: worker
EOF
}

metallb(){
  log "METALLB ..."

  local KIND_SUBNET
  KIND_SUBNET=$(get_subnet "$KIND_NETWORK")
  local METALLB_START
  METALLB_START=$(subnet_to_ip "$KIND_SUBNET" 255.200)
  local METALLB_END
  METALLB_END=$(subnet_to_ip "$KIND_SUBNET" 255.250)

  helm upgrade --install --wait --timeout 35m --atomic --namespace metallb-system --create-namespace \
    --repo https://metallb.github.io/metallb metallb metallb

  kubectl apply -f - <<EOF
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: first-pool
  namespace: metallb-system
spec:
  addresses:
  - $METALLB_START-$METALLB_END
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: layer2
  namespace: metallb-system
spec:
  ipAddressPools:
  - first-pool
EOF
}

ingress(){
  log "INGRESS-NGINX ..."

  # --enable-ssl-passthrough is required by the schema ingress, which sets
  # nginx.ingress.kubernetes.io/ssl-passthrough: "true". Without the flag the
  # controller rejects the ingress and the Schema Registry is not reachable.
  helm upgrade --install --wait --timeout 35m --atomic --namespace ingress-nginx --create-namespace \
    --repo https://kubernetes.github.io/ingress-nginx ingress-nginx ingress-nginx --values - <<EOF
controller:
  extraArgs:
    update-status: "true"
    enable-ssl-passthrough: "true"
EOF
}

hosts(){
  log "HOSTS ..."

  local INGRESS_LB_IP=""
  local i
  for i in $(seq 1 60); do
    INGRESS_LB_IP=$(get_service_lb_ip ingress-nginx ingress-nginx-controller 2>/dev/null || true)
    [ -n "$INGRESS_LB_IP" ] && break
    sleep 2
  done

  if [ -z "$INGRESS_LB_IP" ]; then
    echo "ERROR: could not determine ingress-nginx LoadBalancer IP" >&2
    exit 1
  fi

  echo "$INGRESS_LB_IP schema.$HOSTS_DOMAIN server.$HOSTS_DOMAIN" | sudo tee -a /etc/hosts
}

cleanup(){
  log "CLEANUP ..."

  if grep -q "schema.$HOSTS_DOMAIN" /etc/hosts 2>/dev/null; then
    sudo sed -i "/schema\.${HOSTS_DOMAIN} server\.${HOSTS_DOMAIN}/d" /etc/hosts
  fi

  kind delete cluster --name "$CLUSTER_NAME" || true
}

# RUN

cleanup
network
proxies
cluster
metallb
ingress
hosts

# DONE

log "CLUSTER READY !"