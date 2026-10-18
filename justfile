# Tasks for developing Arkadia; run them in `nix develop` (see flake.nix).
#
# The ark command is the compiler in compiler/, written in Arkadia. It is
# bootstrapped with the Go implementation in bootstrap/: bin/ark0 (Go) builds
# bin/ark1, which builds bin/ark from the same sources.

# Generated programs are built without cgo; build and test the compiler and
# the runtime the same way (its net package would otherwise need a C
# compiler).
export CGO_ENABLED := "0"

# Go sources are gofmt'ed and Arkadia sources ark fmt'ed (tests/errors holds
# deliberately malformed programs).
ark_sources := "lib examples compiler/src tests/run tests/unit"

# List the recipes.
default:
    @just --list

# Build the bootstrap compiler (bootstrap/) into bin/ark0.
bootstrap:
    go build -o bin/ark0 ./bootstrap/cmd/ark

# Bootstrap the compiler (compiler/) into bin/ark.
build: bootstrap
    cd compiler && ../bin/ark0 build -o ../bin/ark1
    cd compiler && ../bin/ark1 build -o ../bin/ark

# Link bin/ark into $GOBIN (it finds lib/ and runtime/ next to its target).
install: build
    #!/usr/bin/env bash
    set -euo pipefail
    dir=$(go env GOBIN); dir=${dir:-$(go env GOPATH)/bin}
    mkdir -p "$dir"
    ln -sf "$PWD/bin/ark" "$dir/ark"
    echo "linked $dir/ark -> $PWD/bin/ark"

# Run the tests (bootstrap, golden programs, compile errors, formatter, unit tests).
test:
    go test ./...

# Check gofmt, go vet and the formatting of the Arkadia sources.
check: build
    #!/usr/bin/env bash
    set -euo pipefail
    files=$(gofmt -l .)
    if [ -n "$files" ]; then echo "not gofmt'ed:"; echo "$files"; exit 1; fi
    go vet ./...
    bin/ark fmt -check {{ark_sources}}

# Format the Go and Arkadia sources.
fmt: build
    gofmt -w .
    bin/ark fmt {{ark_sources}}

# Rebuild bin/ark with itself and check that it reproduces itself.
fixpoint: build
    cd compiler && ../bin/ark build -o ../bin/ark2
    cd compiler && ../bin/ark1 emit > ../bin/ark1.go.txt
    cd compiler && ../bin/ark emit > ../bin/ark.go.txt
    cd compiler && ../bin/ark2 emit > ../bin/ark2.go.txt
    cmp bin/ark1.go.txt bin/ark.go.txt
    cmp bin/ark.go.txt bin/ark2.go.txt
    cmp bin/ark bin/ark2
    @echo "fixpoint OK: ark reproduces itself"

# Package bin/ark with lib/ and runtime/rt into dist/arkadia-VERSION-OS-ARCH.tar.gz
# for each release target (the compiler is cross-compiled from the Go code
# that bin/ark generates for itself).
dist: build
    #!/usr/bin/env bash
    set -euo pipefail
    version=$(bin/ark version | cut -d' ' -f2)
    root=$PWD work=$PWD/out/dist-go
    rm -rf dist out/dist-go
    (cd compiler && ../bin/ark build -work "$work" -o "$work/ark")
    for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
        os=${target%/*} arch=${target#*/}
        name=arkadia-$version-$os-$arch
        pkg=out/dist/$name
        rm -rf "$pkg"
        mkdir -p "$pkg/bin" "$pkg/runtime/rt"
        (cd "$work" && GOOS=$os GOARCH=$arch GOWORK=off GOFLAGS=-mod=mod go build -trimpath -ldflags='-s -w' -o "$root/$pkg/bin/ark" .)
        cp -r lib "$pkg/lib"
        find runtime/rt -name '*.go' ! -name '*_test.go' -exec cp {} "$pkg/runtime/rt/" \;
        cp LICENSE README.md CHANGELOG.md "$pkg/"
        mkdir -p dist
        tar -C out/dist -czf "dist/$name.tar.gz" "$name"
        echo "dist/$name.tar.gz"
    done
    (cd dist && sha256sum *.tar.gz > SHA256SUMS)
