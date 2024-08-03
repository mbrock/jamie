# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
BINARY_NAME=jamie

# ONNX Runtime flags
CGO_CFLAGS=$(shell pkg-config --cflags libonnxruntime)
CGO_LDFLAGS=$(shell pkg-config --libs libonnxruntime)

all: test build

build:
	CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" \
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
