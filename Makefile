.PHONY: build install test check selfhost bootstrap

build:
	go build -o bin/ark ./cmd/ark

install:
	go install ./cmd/ark

test:
	go test ./...

check:
	gofmt -l . && go vet ./...

# The self-hosted compiler (selfhost/), built by the Go implementation.
selfhost: build
	cd selfhost && ../bin/ark build -o ../bin/arkc

# Rebuild the self-hosted compiler with itself twice and check that the
# generations agree (the bootstrap reaches a fixpoint).
bootstrap: selfhost
	cd selfhost && ../bin/arkc build -o ../bin/arkc2
	cd selfhost && ../bin/arkc2 build -o ../bin/arkc3
	cd selfhost && ../bin/arkc emit > ../bin/arkc1.go.txt
	cd selfhost && ../bin/arkc2 emit > ../bin/arkc2.go.txt
	cd selfhost && ../bin/arkc3 emit > ../bin/arkc3.go.txt
	cmp bin/arkc1.go.txt bin/arkc2.go.txt && cmp bin/arkc2.go.txt bin/arkc3.go.txt && cmp bin/arkc2 bin/arkc3
	@echo "bootstrap OK: arkc reproduces itself"
