package main

import (
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/randsw/kafka-http-server/logger"
	"go.uber.org/zap"
)

type statHandler struct {
	Data *statistic
}

type payload struct {
	Topic     string  `json:"topic"`
	Partition int     `json:"partition"`
	Message   message `json:"message"`
	Key       string  `json:"key"`
}

type message struct {
	User  string `json:"user"`
	Car   string `json:"car"`
	Color string `json:"color"`
}

type statistic struct {
	Total          int         `json:"total"`
	TotalPartition map[int]int `json:"total_partition"`
	LastMessage    *message    `json:"last_message"`
}

func (stat *statHandler) stats(w http.ResponseWriter, r *http.Request) {
	// Display statistic information
	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(stat.Data)
	if err != nil {
		logger.Error("Fail encode statistics", zap.String("err", err.Error()))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (stat *statHandler) kafkaStatus(w http.ResponseWriter, r *http.Request) {

	var Payload payload

	err := json.NewDecoder(r.Body).Decode(&Payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	logger.Info("Received request", zap.String("Topic", Payload.Topic), zap.Int("Partition", Payload.Partition), zap.String("Key", Payload.Key),
		zap.String("User", Payload.Message.User), zap.String("Car", Payload.Message.Car), zap.String("Color", Payload.Message.Color))

	// Add data to statistic
	stat.Data.Total += 1
	if _, ok := stat.Data.TotalPartition[Payload.Partition]; !ok {
		stat.Data.TotalPartition[Payload.Partition] = 1
	}
	stat.Data.TotalPartition[Payload.Partition] += 1
	stat.Data.LastMessage = &Payload.Message
	w.WriteHeader(http.StatusOK)
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
	// Create empty struct
	data := &statistic{
		LastMessage:    &message{},
		Total:          0,
		TotalPartition: map[int]int{},
	}
	h := &statHandler{Data: data}
	mux := mux.NewRouter()
	mux.HandleFunc("/stats", h.kafkaStatus)
	mux.HandleFunc("/", h.stats)
	//Start healthz http server
	servingAddress := ":8080"
	srv := &http.Server{
		Addr: servingAddress,
		// Good practice to set timeouts to avoid Slowloris attacks.
		WriteTimeout: time.Second * 15,
		ReadTimeout:  time.Second * 15,
		IdleTimeout:  time.Second * 60,
		Handler:      mux, // Pass our instance of gorilla/mux in.
	}
	logger.Info("Start serving http request...", zap.String("address", servingAddress))
	err := srv.ListenAndServe()
	if err != nil {
		logger.Error("Fail to start http server", zap.String("err", err.Error()))
	}
	<-done
}
