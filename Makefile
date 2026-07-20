.PHONY: install build vet test test-short tidy

install:
	go install ./cmd/broker

build:
	go build ./...

vet:
	go vet ./...

test:
	go test ./...

test-short:
	go test -short ./...

tidy:
	go mod tidy