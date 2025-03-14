GO_VERSION := 1.24

.PHONY: build
build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build  -a -o kafka-consumer main.go
