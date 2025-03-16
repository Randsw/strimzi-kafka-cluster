package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/schemaregistry"
	"github.com/confluentinc/confluent-kafka-go/v2/schemaregistry/serde"
	"github.com/confluentinc/confluent-kafka-go/v2/schemaregistry/serde/jsonschema"
	"github.com/randsw/kafka-consumer/logger"
	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
)

type message struct {
	User  string `json:"user"`
	Car   string `json:"car"`
	Color string `json:"color"`
}

type payload struct {
	Topic     string  `json:"topic"`
	Partition int     `json:"partition"`
	Message   message `json:"message"`
	Key       string  `json:"key"`
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

func main() {
	//Loger Initialization
	logger.InitLogger()
	defer logger.CloseLogger()

	BootstrapServers := os.Getenv("BOOTSTRAP_SERVERS")
	topic := os.Getenv("TOPIC")
	groupID := os.Getenv("GROUP_ID")
	schemaRegistryURL := os.Getenv("SCHEMA_REGISTRY_URL")
	outAddress := os.Getenv("OUT_ADDRESS")

	var wg sync.WaitGroup

	// SIGTERM
	cancelChan := make(chan os.Signal, 1)
	signal.Notify(cancelChan, syscall.SIGTERM, syscall.SIGINT)
	done := make(chan bool, 1)

	go func() {
		sig := <-cancelChan
		logger.Info("Caught signal", zap.String("Signal", sig.String()))
		logger.Info("Wait for 1 second to finish processing")
		time.Sleep(1 * time.Second)
		logger.Info("Exiting......")
		done <- true
		os.Exit(0)
	}()

	client, err := schemaregistry.NewClient(schemaregistry.NewConfig(fmt.Sprintf("http://%s", schemaRegistryURL)))

	if err != nil {
		logger.Error("Failed to create schema registry client: %s\n", zap.String("err", err.Error()))
		os.Exit(1)
	}

	deser, err := jsonschema.NewDeserializer(client, serde.ValueSerde, jsonschema.NewDeserializerConfig())

	if err != nil {
		logger.Error("Failed to create serializer: %s\n", zap.String("err", err.Error()))
		os.Exit(1)
	}

	dialer := &kafka.Dialer{
		Timeout:   10 * time.Second,
		DualStack: true,
		TLS:       newTLSCOnfig(),
	}

	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{BootstrapServers},
		GroupID: groupID,
		Topic:   topic,
		Dialer:  dialer,
	})
	defer r.Close()

	wg.Add(1)
	go func() {
		for {
			defer wg.Done()
			// make a new reader that consumes from topic-A

			m, err := r.ReadMessage(context.Background())
			if err != nil {
				logger.Error("Failed to read message", zap.String("err", err.Error()))
			}
			value := message{}
			err = deser.DeserializeInto(topic, m.Value, &value)
			if err != nil {
				logger.Error("Failed to deserialize payload", zap.String("err", err.Error()))
			}
			logger.Info("Message from kafka", zap.String("Topic", m.Topic), zap.Int("Partition", m.Partition),
				zap.Int64("Offset", m.Offset), zap.String("Key", string(m.Key)), zap.String("Value", fmt.Sprintf("%#v", value)))

			//Send to http server
			dnsName := fmt.Sprintf("http://%s/stats", outAddress)
			jsonData, err := json.Marshal(&payload{
				Message:   value,
				Topic:     m.Topic,
				Partition: m.Partition,
				Key:       string(m.Key),
			})
			if err != nil {
				logger.Error("Error while encoding JSON", zap.String("err", err.Error()))
				continue
			}
			req, err := http.NewRequest("POST", dnsName, bytes.NewBuffer(jsonData))
			if err != nil {
				logger.Error("Error while creating JSON request", zap.String("err", err.Error()))
				continue
			}
			req.Header.Set("Content-Type", "application/json")
			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				logger.Error("Error while posting JSON request", zap.String("err", err.Error()))
				continue
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				logger.Error("Web respond with error", zap.Int("StatusCode", resp.StatusCode))
			} else {
				logger.Error("Send payload to web")
			}
		}
	}()
	// Wait for SITERM or SIGINT
	<-done
}
