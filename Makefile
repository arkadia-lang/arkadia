.PHONY: build install test check

build:
	go build -o bin/ark ./cmd/ark

install:
	go install ./cmd/ark

test:
	go test ./...

check:
	gofmt -l . && go vet ./...
