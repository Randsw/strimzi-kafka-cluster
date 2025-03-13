#!/usr/bin/env bash

set -e

# Deploy Kafka CLuster

helm install --wait --timeout 35m --atomic --namespace kafka --create-namespace \
  strimzi-operator oci://quay.io/strimzi-helm/strimzi-kafka-operator --values - <<EOF
replicas: 3
EOF

cat << EOF | kubectl apply -f -
apiVersion: kafka.strimzi.io/v1beta2
kind: KafkaNodePool
metadata:
  name: controller
  namespace: kafka
  labels:
    strimzi.io/cluster: kafka-cluster
spec:
  replicas: 3
  roles:
    - controller
  storage:
    type: jbod
    volumes:
      - id: 0
        type: ephemeral
        kraftMetadata: shared
---
apiVersion: kafka.strimzi.io/v1beta2
kind: KafkaNodePool
metadata:
  name: broker
  namespace: kafka
  labels:
    strimzi.io/cluster: kafka-cluster
spec:
  replicas: 3
  roles:
    - broker
  storage:
    type: jbod
    volumes:
      - id: 0
        type: ephemeral
        kraftMetadata: shared
---
apiVersion: kafka.strimzi.io/v1beta2
kind: Kafka
metadata:
  name: kafka-cluster
  namespace: kafka
  annotations:
    strimzi.io/node-pools: enabled
    strimzi.io/kraft: enabled
spec:
  kafka:
    version: 3.9.0
    metadataVersion: 3.9-IV0
    listeners:
      - name: plain
        port: 9092
        type: internal
        tls: false
      - name: tls
        port: 9093
        type: internal
        tls: true
        authentication:
          type: tls
    authorization:
      type: simple
      superUsers:
        - CN=root
    config:
      offsets.topic.replication.factor: 3
      transaction.state.log.replication.factor: 3
      transaction.state.log.min.isr: 2
      default.replication.factor: 3
      min.insync.replicas: 2
  entityOperator:
    topicOperator: {}
    userOperator: {}
EOF

# Deploy Schema Registry and proper Topic and User
helm upgrade --install --wait --timeout 35m --atomic --namespace kafka --create-namespace \
  --repo https://lsst-sqre.github.io/charts/ ssr strimzi-registry-operator  --values - <<EOF
clusterName: kafka-cluster
clusterNamespace: kafka
operatorNamespace: kafka
EOF

cat <<EOF | kubectl apply -n kafka -f -
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: strimzi-registry-operator
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: strimzi-registry-operator
rules:

  - apiGroups: [apiextensions.k8s.io]
    resources: [customresourcedefinitions]
    verbs: [list, get, watch]

  # Kopf: posting the events about the handlers progress/errors.
  - apiGroups: [events.k8s.io]
    resources: [events]
    verbs: [create]
  - apiGroups: [""]
    resources: [events]
    verbs: [create]

  # Application: watching & handling for the custom resource we declare.
  - apiGroups: [roundtable.lsst.codes]
    resources: [strimzischemaregistries]
    verbs: [get, list, watch, patch]

  # Access to the built-in resources the operator manages
  - apiGroups: [""]
    resources: [secrets, configmaps, services]
    verbs: [get, list, watch, patch, create]
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: [get, list, watch, patch, create]

  # Access to the KafkaUser resource
  - apiGroups: [kafka.strimzi.io]
    resources: [kafkausers, kafkas]
    verbs: [list, get, watch]

  - apiGroups: [""]
    resources: ["namespaces"]
    verbs: [get, list, "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: strimzi-registry-operator
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: strimzi-registry-operator
subjects:
  - kind: ServiceAccount
    name: strimzi-registry-operator
    namespace: kafka
EOF


cat << EOF | kubectl apply -f -
apiVersion: kafka.strimzi.io/v1beta2
kind: KafkaTopic
metadata:
  name: registry-schemas
  namespace: kafka
  labels:
    strimzi.io/cluster: kafka-cluster
spec:
  partitions: 1
  replicas: 3
  config:
    # http://kafka.apache.org/documentation/#topicconfigs
    cleanup.policy: compact
---
apiVersion: kafka.strimzi.io/v1beta2
kind: KafkaUser
metadata:
  name: confluent-schema-registry
  namespace: kafka
  labels:
    strimzi.io/cluster: kafka-cluster
spec:
  authentication:
    type: tls
  authorization:
    # Official docs on authorizations required for the Schema Registry:
    # https://docs.confluent.io/current/schema-registry/security/index.html#authorizing-access-to-the-schemas-topic
    type: simple
    acls:
      # Allow all operations on the registry-schemas topic
      # Read, Write, and DescribeConfigs are known to be required
      - resource:
          type: topic
          name: registry-schemas
          patternType: literal
        operations:
          - All
        type: allow
        host: "*"
      # Allow all operations on the schema-registry* group
      - resource:
          type: group
          name: schema-registry
          patternType: prefix
        operations: 
          - All
        type: allow
        host: "*"
      # Allow Describe on the __consumer_offsets topic
      - resource:
          type: topic
          name: __consumer_offsets
          patternType: literal
        operations: 
          - Describe
        type: allow
        host: "*"
EOF

cat << EOF | kubectl apply -f -
apiVersion: roundtable.lsst.codes/v1beta1
kind: StrimziSchemaRegistry
metadata:
  name: confluent-schema-registry
  namespace: kafka
spec:
  strimziVersion: v1beta2
  listener: tls
EOF

