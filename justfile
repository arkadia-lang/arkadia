# Tasks for developing Arkadia; run them in `nix develop` (see flake.nix).

# Generated programs are built without cgo; build and test the compiler and
# the runtime the same way (its net package would otherwise need a C
# compiler).
export CGO_ENABLED := "0"

# Go sources are gofmt'ed and Arkadia sources ark fmt'ed (tests/errors holds
# deliberately malformed programs).
ark_sources := "lib examples selfhost/src tests/run tests/unit"

# List the recipes.
default:
    @just --list

# Build the ark command into bin/ark.
build:
    go build -o bin/ark ./bootstrap/cmd/ark

# Install the ark command into $GOBIN.
install:
    go install ./bootstrap/cmd/ark

# Run the tests (compiler, golden programs, unit tests, self-hosted compiler).
test:
    go test ./...

# Check gofmt, go vet and the formatting of the Arkadia sources.
check:
    #!/usr/bin/env bash
    set -euo pipefail
    files=$(gofmt -l .)
    if [ -n "$files" ]; then echo "not gofmt'ed:"; echo "$files"; exit 1; fi
    go vet ./...
    go run ./bootstrap/cmd/ark fmt -check {{ark_sources}}

# Format the Go and Arkadia sources.
fmt:
    gofmt -w .
    go run ./bootstrap/cmd/ark fmt {{ark_sources}}

# Build the self-hosted compiler (selfhost/) into bin/arkc.
selfhost: build
    cd selfhost && ../bin/ark build -o ../bin/arkc

# Rebuild arkc with itself twice and check that the generations agree.
bootstrap: selfhost
    cd selfhost && ../bin/arkc build -o ../bin/arkc2
    cd selfhost && ../bin/arkc2 build -o ../bin/arkc3
    cd selfhost && ../bin/arkc emit > ../bin/arkc1.go.txt
    cd selfhost && ../bin/arkc2 emit > ../bin/arkc2.go.txt
    cd selfhost && ../bin/arkc3 emit > ../bin/arkc3.go.txt
    cmp bin/arkc1.go.txt bin/arkc2.go.txt
    cmp bin/arkc2.go.txt bin/arkc3.go.txt
    cmp bin/arkc2 bin/arkc3
    @echo "bootstrap OK: arkc reproduces itself"
