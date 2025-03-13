helm install strimzi-cluster-operator oci://quay.io/strimzi-helm/strimzi-kafka-operator


helm upgrade --install --wait --timeout 35m --atomic --namespace kafka --create-namespace \
  --repo oci://quay.io/strimzi-helm/strimzi-kafka-operator strimzi-cluster-operator strimzi-kafka-operator --values - <<EOF
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


helm upgrade --install --wait --timeout 35m --atomic --namespace ssr --create-namespace
--repo https://lsst-sqre.github.io/charts/ ssr strimzi-registry-operator  --values - <<EOF
clusterName: kafka-cluster
clusterNamespace: kafka
operatorNamespace: ssr
EOF

cat << EOF | kubectl apply -f -
apiVersion: kafka.strimzi.io/v1beta2
kind: KafkaTopic
metadata:
  name: registry-schemas
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
        operation: All
        type: allow
      # Allow all operations on the schema-registry* group
      - resource:
          type: group
          name: schema-registry
          patternType: prefix
        operation: All
        type: allow
      # Allow Describe on the __consumer_offsets topic
      - resource:
          type: topic
          name: __consumer_offsets
          patternType: literal
        operation: Describe
        type: allow
EOF

cat << EOF | kubectl apply -f -
apiVersion: roundtable.lsst.codes/v1beta1
kind: StrimziSchemaRegistry
metadata:
  name: confluent-schema-registry
spec:
  strimziVersion: v1beta2
  listener: tls
EOF

