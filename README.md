# Use Strimzi to setup Kafka cluster with Schema Registry in Kubernetes

## Requirements

- docker
- kubectl
- kind cli
- helm

## Setup kubernetes cluster

Run `./cluster-setup.sh` and you got 1 control-plane nodes and 3 worker nodes kubernetes cluster with installed ingress-nginx, metallb and 4 proxy image repository in docker containers in one network


