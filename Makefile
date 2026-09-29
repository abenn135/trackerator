.PHONY: build test

build:
	mkdir -p bin
	go build -o bin/trackerator .

test:
	go test ./...
