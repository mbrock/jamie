GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
BINARY_NAME=jamie

export CGO_CFLAGS=$(shell pkg-config --cflags-only-I libonnxruntime)
export CGO_LDFLAGS=$(shell pkg-config --libs-only-L libonnxruntime)

all: test build

build: sqlc templ
	$(GOBUILD) -o $(BINARY_NAME) -v

test: sqlc templ
	$(GOTEST) -v -count=1 ./...

serve:
	templ generate --watch --cmd="go run main.go serve -p 4445"

clean:
	$(GOCLEAN)
	rm -f $(BINARY_NAME)

run: build
	./$(BINARY_NAME)

.PHONY: all build test clean sqlc templ init

sqlc:
	sqlc generate

templ:
	templ generate

init: sqlc templ
	go mod tidy
