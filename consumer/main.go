package main

import (
	"bytes"
	"context"
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

	client, err := schemaregistry.NewClient(schemaregistry.NewConfig(schemaRegistryURL))

	if err != nil {
		logger.Error("Failed to create schema registry client: %s\n", zap.String("err", err.Error()))
		os.Exit(1)
	}

	deser, err := jsonschema.NewDeserializer(client, serde.ValueSerde, jsonschema.NewDeserializerConfig())

	if err != nil {
		logger.Error("Failed to create serializer: %s\n", zap.String("err", err.Error()))
		os.Exit(1)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		// make a new reader that consumes from topic-A
		r := kafka.NewReader(kafka.ReaderConfig{
			Brokers:  []string{BootstrapServers},
			GroupID:  groupID,
			Topic:    topic,
			MaxBytes: 10e6, // 10MB
		})
		m, err := r.ReadMessage(context.Background())
		if err != nil {
			logger.Error("Failed to deserialize payload", zap.String("err", err.Error()))
		}
		value := message{}
		err = deser.DeserializeInto(topic, m.Value, &value)
		if err != nil {
			logger.Error("Failed to deserialize payload", zap.String("err", err.Error()))
		} else {
			logger.Info("%% Message on %s:\n%+v\n", zap.String("Topic", m.Topic), zap.String("Value", fmt.Sprintf("%#v", value)))
		}
		logger.Info("message at topic/partition/offset %v/%v/%v: %s = %s\n", zap.String("Topic", m.Topic), zap.Int("Partition", m.Partition),
			zap.Int64("Offset", m.Offset), zap.String("Key", string(m.Key)), zap.String("Value", fmt.Sprintf("%#v", value)))

		// Send to http server
		dnsName := fmt.Sprintf("http://%s", outAddress)
		jsonData, err := json.Marshal(&payload{
			Message:   value,
			Topic:     m.Topic,
			Partition: m.Partition,
			Key:       string(m.Key),
		})
		if err != nil {
			logger.Error("Error while encoding JSON", zap.String("err", err.Error()))
			return
		}
		req, err := http.NewRequest("POST", dnsName, bytes.NewBuffer(jsonData))
		if err != nil {
			logger.Error("Error while creating JSON request", zap.String("err", err.Error()))
			return
		}
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			logger.Error("Error while posting JSON request", zap.String("err", err.Error()))
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			logger.Error("Web respond with error", zap.Int("StatusCode", resp.StatusCode))
		} else {
			logger.Error("Send payload to web")
		}
		defer r.Close()
	}()
	<-done
}
