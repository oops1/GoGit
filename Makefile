BINARY   := gogit
MODULE   := github.com/oops1/gogit
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X $(MODULE)/internal/version.Version=$(VERSION)
GOFLAGS  := -trimpath
export CGO_ENABLED := 0

.PHONY: all build build-windows build-linux run test test-race cover cover-html vet lint fmt check clean

all: check build

build:
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/$(BINARY)$(EXE) ./cmd/gogit

build-windows:
	GOOS=windows GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS) -H windowsgui" -o dist/windows-amd64/$(BINARY).exe ./cmd/gogit

build-linux:
	GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o dist/linux-amd64/$(BINARY) ./cmd/gogit

run:
	go run ./cmd/gogit

test:
	go test -count=1 ./...

test-race:
	go test -count=1 -race ./...

cover:
	go test -count=1 -coverprofile=cover.out -covermode=atomic ./...
	go tool cover -func=cover.out | tail -1

cover-html: cover
	go tool cover -html=cover.out -o coverage.html

vet:
	go vet -unsafeptr=false ./...

lint:
	golangci-lint run ./...

fmt:
	gofmt -l -w .

check: fmt vet test-race cover
	GOOS=linux go build ./...
	GOOS=windows go build ./...

clean:
	rm -rf bin dist cover.out coverage.html
