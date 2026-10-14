# Use Strimzi to setup Kafka cluster with Schema Registry in Kubernetes

Producer, Consumer and Schema Registry use mTLS to authentificate and authorize in Kafka Cluser.

Alternative authentication methods are also supported: SASL_SSL (SCRAM-SHA-512 over TLS) and SASL_PLAINTEXT (SCRAM-SHA-512 over plaintext).

## Requirements

- docker
- kubectl
- kind cli
- helm

## Setup kubernetes cluster

Run `./cluster-setup.sh` and you got 1 control-plane nodes and 3 worker nodes kubernetes cluster with installed ingress-nginx, metallb and 4 proxy image repository in docker containers in one network

## Deploy Kafka and Schema Registry

Run `./kafka-cluster.sh` to deploy 3 Kafka brocker and 3 Kafka KRaft control node and Schema Registry. Schema-registry required special user and topic to storage his data in Kafka so we deploy them too.

Schema-Registry deployed using [ssr-operator.](https://github.com/Randsw/schema-registry-operator-strimzi)

User deployed usind `KafkaUser` CR and named `confluent-schema-registry`.
Topic deployed usind `KafkaTopic` CR and named `registry-schemas`.

### Authentication methods

By default `./kafka-cluster.sh` deploys the cluster with TLS (mTLS client certificate) authentication. Two alternative deployment scripts with SASL authentication are available:

| Script | Kafka listener | `securityprotocol` | `KafkaUser` authentication | Description |
| --- | --- | --- | --- | --- |
| `./kafka-cluster.sh` | `tls` (port 9093, TLS) | `SSL` | `tls` | mTLS client certificate authentication. The operator builds a keystore and truststore from the KafkaUser secret. |
| `./kafka-cluster-sasl-ssl.sh` | `saslssl` (port 9093, TLS) | `SASL_SSL` | `scram-sha-512` | SCRAM-SHA-512 authentication over TLS. The operator builds only a truststore and reads the SCRAM credentials directly from the KafkaUser secret. |
| `./kafka-cluster-sasl-plain.sh` | `saslplain` (port 9092, plaintext) | `SASL_PLAINTEXT` | `scram-sha-512` | SCRAM-SHA-512 authentication over plaintext transport. No certificates or keystore are required. |

Run the script for the authentication method you need, e.g. `./kafka-cluster-sasl-ssl.sh` for SASL_SSL or `./kafka-cluster-sasl-plain.sh` for SASL_PLAINTEXT. Each script deploys the full stack: Strimzi operator, Kafka cluster with the corresponding listener, `registry-schemas` topic, `confluent-schema-registry` KafkaUser, ssr-operator and the Schema Registry with an ingress.

:warning: Only one of these scripts should be run against the same cluster, since they deploy resources with identical names.

### StrimziSchemaRegistry configuration

The Schema Registry is deployed with a `StrimziSchemaRegistry` CR. Its `spec` supports the following fields:

| Field | Description | Default |
| --- | --- | --- |
| `securehttp` | Enable TLS on the Schema Registry REST API endpoint (required). | — |
| `listener` | Name of the Kafka listener the Schema Registry connects to. | `tls` |
| `compatibilitylevel` | Default schema compatibility level: `none`, `backward`, `backward_transitive`, `forward`, `forward_transitive`, `full`, `full_transitive`. | `forward` |
| `securityprotocol` | Kafka security protocol: `SSL`, `SASL_SSL`, `PLAINTEXT`, `SASL_PLAINTEXT`. | `SSL` |
| `heapopts` | JVM heap options for the Schema Registry, e.g. `-Xms512M -Xmx512M`. | `-Xms512M -Xmx512M` |
| `tlssecretname` | Name of the secret with the JKS keystore used by the REST API. Leave empty to let the operator generate one signed by the Kafka cluster CA. | `""` |
| `template` | Kubernetes pod template for the Schema Registry pod (required). | — |

See the [operator documentation](https://github.com/Randsw/schema-registry-operator-strimzi) for details.

Now we have running Kafka Cluser with Schema Registry inside our `kafka` namespace.

You can access Schema registry at `http://schema.kind.cluster` :warning: Schema registry used HTTP/2

To load schema in schema registry see example in `schema-install.sh`

## Deploy example producer

First we must create example topic and user for connection to Kafka

Run `kubectl apply -f ./producer/manifests/kafka-user.yaml` to create user named `producer-user`

Run `kubectl apply -f ./producer/manifests/kafka-topic.yaml` to create topic named `example-topic` with 3 partition

Now we ready to deploy our producer:

Run `kubectl apply -f ./producer/manifests/deployment.yaml` to create producer.

Producer code can be find in `./producer` folder

It generates message with 1 second delay and random key from a list `Key-1, Key-2, Key-3`.

As a value we use struct which is filled also randomly:

```go

type message struct {
    User  string `json:"user"`
    Car   string `json:"car"`
    Color string `json:"color"`
}

```

Balance algoritm is `Hash` so message with equal key go to same partition.

## Deploy statistic server

Run `kubectl apply -f ./http_server/manifests/deployment.yaml` to deploy topic statistic server

Run `kubectl apply -f ./http_server/manifests/service.yaml` to deploy service

Run `kubectl apply -f ./http_server/manifests/ingress.yaml` to deploy ingress

Now statistic server opens at `server.kind.cluster`

## Deploy consumer

In this example we create 3 consumer in one consumer group named `example-group`

So each consumer will be consume one patrition.

Each consumer send information at statistic server so we can see how many message we receive total and by patrition.

Also we can see last message.

To deploy consumer group:

Run `kubectl apply -f ./consumer/manifests/kafka-user.yaml` to deploy consumer user named `consumer-user`.

Run `kubectl apply -f ./consumer/manifests/deployment.yaml` to deploy consumer group with 3 instances.

## Result

Example view of statistic server:

![statistic server](image/server.png)

Kubernetes pod:

![kubernetes pods](image/kubernetes.png)
