BINARY   := gogit
MODULE   := github.com/oops1/gogit
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X $(MODULE)/internal/version.Version=$(VERSION)
GOFLAGS  := -trimpath
export CGO_ENABLED := 0

.PHONY: all build build-windows build-linux winres release-local run test test-race test-oracle cover cover-html vet lint fmt check clean

all: check build

build:
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/$(BINARY)$(EXE) ./cmd/gogit

winres:
	cd cmd/gogit && go run github.com/tc-hib/go-winres@v0.3.3 make --in winres/winres.json --arch amd64

build-windows: winres
	GOOS=windows GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS) -H windowsgui" -o dist/windows-amd64/$(BINARY).exe ./cmd/gogit

build-linux:
	GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o dist/linux-amd64/$(BINARY) ./cmd/gogit

run:
	go run ./cmd/gogit

test:
	go test -count=1 ./...

test-race:
	CGO_ENABLED=1 go test -count=1 -race ./...

test-oracle:
	go test -count=1 -tags oracle ./...

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

check: fmt vet test-race test-oracle cover
	GOOS=linux go build ./...
	GOOS=windows go build ./...

release-local: build-windows build-linux
	GOOS=linux GOARCH=arm64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o dist/linux-arm64/$(BINARY) ./cmd/gogit
	mkdir -p dist/windows-amd64 dist/linux-amd64 dist/linux-arm64
	cp LICENSE NOTICE dist/windows-amd64/ && cp LICENSE NOTICE dist/linux-amd64/ && cp LICENSE NOTICE dist/linux-arm64/
	cd dist/windows-amd64 && zip -q -r ../gogit-$(VERSION)-windows-amd64.zip gogit.exe LICENSE NOTICE && cd ../..
	cd dist/linux-amd64 && tar czf ../gogit-$(VERSION)-linux-amd64.tar.gz gogit LICENSE NOTICE && cd ../..
	cd dist/linux-arm64 && tar czf ../gogit-$(VERSION)-linux-arm64.tar.gz gogit LICENSE NOTICE && cd ../..
	cd dist && sha256sum gogit-*.zip gogit-*.tar.gz > SHA256SUMS && cat SHA256SUMS && cd ..

clean:
	rm -rf bin dist cover.out coverage.html cmd/gogit/*.syso
