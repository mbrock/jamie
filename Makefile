# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
BINARY_NAME=jamie

# ONNX Runtime flags
export CGO_CFLAGS=$(shell pkg-config --cflags libonnxruntime)
export CGO_LDFLAGS=$(shell pkg-config --libs libonnxruntime)

all: test build

build:
	$(GOBUILD) -o $(BINARY_NAME) -v

test:
	$(GOTEST) -v -count=1 ./...

clean:
	$(GOCLEAN)
	rm -f $(BINARY_NAME)

run: build
	./$(BINARY_NAME)

deps:
	$(GOGET) ./...
	$(GOGET) github.com/bwmarrin/discordgo
	$(GOGET) github.com/nats-io/nats.go
