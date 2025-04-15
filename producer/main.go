package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/schemaregistry"
	"github.com/confluentinc/confluent-kafka-go/v2/schemaregistry/serde"
	"github.com/confluentinc/confluent-kafka-go/v2/schemaregistry/serde/jsonschema"
	"go.uber.org/zap"

	"github.com/randsw/kafka-producer/logger"

	kafka "github.com/segmentio/kafka-go"
)

type message struct {
	User  string `json:"user"`
	Car   string `json:"car"`
	Color string `json:"color"`
}

func newTLSCOnfig() *tls.Config {
	// Only the <cluster_name>-cluster-ca-cert secret is required by clients.
	//ca.crt The current certificate for the cluster CA.
	cert, err := os.ReadFile("/tmp/ca/ca.crt")
	if err != nil {
		logger.Error("could not open CA certificate file: %v", zap.String("err", err.Error()))
		return nil
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(cert)
	// Secret name 	Field within secret 	Description

	// <user_name> 	user.p12                PKCS #12 store for storing certificates and keys.

	//              user.password         	Password for protecting the PKCS #12 store.

	//              user.crt           	    Certificate for the user, signed by the clients CA

	//              user.key            	Private key for the user
	cer, err := tls.LoadX509KeyPair("/tmp/client/user.crt", "/tmp/client/user.key")
	if err != nil {
		logger.Error("could not open client ertificate file: %v", zap.String("err", err.Error()))
		return nil
	}
	config := &tls.Config{RootCAs: caCertPool,
		Certificates: []tls.Certificate{cer}}
	return config
}

func newKafkaWriter(kafkaURL, topic string) *kafka.Writer {
	return &kafka.Writer{
		Addr:     kafka.TCP(kafkaURL),
		Topic:    topic,
		Balancer: &kafka.Hash{},
		Transport: &kafka.Transport{
			TLS: newTLSCOnfig(),
		},
	}
}

func Serialize(m *message, ser *jsonschema.Serializer, topic string) []byte {
	payload, err := ser.Serialize(topic, &m)
	if err != nil {
		logger.Error("Failed to serialize payload: %s\n", zap.String("err", err.Error()))
		//os.Exit(1)
	}
	return payload
}

func main() {
	//Loger Initialization
	logger.InitLogger()
	defer logger.CloseLogger()
	//Create channel for signal
	cancelChan := make(chan os.Signal, 1)
	// catch SIGETRM or SIGINTERRUPT
	signal.Notify(cancelChan, syscall.SIGTERM, syscall.SIGINT)
	done := make(chan bool, 1)

	var wg sync.WaitGroup

	go func() {
		sig := <-cancelChan
		logger.Info("Caught signal", zap.String("Signal", sig.String()))
		logger.Info("Wait for 1 second to finish processing")
		time.Sleep(1 * time.Second)
		logger.Info("Exiting.....")
		// shutdown other goroutines gracefully
		// close other resources
		done <- true
		os.Exit(0)
	}()
	kafkaURL := os.Getenv("KAFKA_URL")
	topic := os.Getenv("TOPIC")
	schemaRegistryURL := os.Getenv("SCHEMA_REGISTRY_URL")
	writer := newKafkaWriter(kafkaURL, topic)
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	defer writer.Close()
	schemaregistryConfig := schemaregistry.NewConfig(fmt.Sprintf("https://%s", schemaRegistryURL))
	schemaregistryConfig.SslCaLocation = "/tmp/ca/ca.crt"

	client, err := schemaregistry.NewClient(schemaregistryConfig)

	if err != nil {
		logger.Error("Failed to create schema registry client: %s\n", zap.String("err", err.Error()))
		//os.Exit(1)
	}
	// Subject name in schema registry must match topic name!!!!!!!
	ser, err := jsonschema.NewSerializer(client, serde.ValueSerde, jsonschema.NewSerializerConfig())

	if err != nil {
		logger.Error("Failed to create serializer: %s\n", zap.String("err", err.Error()))
		os.Exit(1)
	}

	ID := []string{"1", "2", "3", "4"}
	names := []string{"John", "Mike", "Dwight", "Pam", "Kevin"}
	cars := []string{"Kia", "Ford", "BMW"}
	color := []string{"Red", "Black", "White", "Blue", "Green", "Gray"}
	wg.Add(1)
	go func() {
		for {
			defer wg.Done()
			randomValue := r.Intn(3)
			key := fmt.Sprintf("Key-%s", ID[randomValue])
			val := &message{
				User:  names[r.Intn(len(names)-1)],
				Car:   cars[r.Intn(len(cars)-1)],
				Color: color[r.Intn(len(color)-1)],
			}
			msg := kafka.Message{
				Key:   []byte(key),
				Value: Serialize(val, ser, topic),
			}
			err := writer.WriteMessages(context.Background(), msg)
			if err != nil {
				logger.Error("Failed to create serializer: %s\n", zap.String("err", err.Error()))
				return
			} else {
				logger.Info("produced", zap.String("key", key), zap.String("message", fmt.Sprintf("%s", val)))
			}
			time.Sleep(1 * time.Second)
		}
	}()
	<-done
}
