.PHONY: build build-notray test install

build:
	go build -o bin/orei-kvm ./cmd/orei-kvm

build-notray:
	go build -tags notray -o bin/orei-kvm ./cmd/orei-kvm

test:
	go test ./...

install:
	go install ./cmd/orei-kvm
