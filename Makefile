GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
BINARY_NAME=jamie

#export CGO_CFLAGS=$(shell pkg-config --cflags-only-I libonnxruntime)
#export CGO_LDFLAGS=$(shell pkg-config --libs-only-L libonnxruntime)

all: test build

build: sqlc templ
	$(GOBUILD) -o $(BINARY_NAME) -v

test: sqlc templ
	$(GOTEST) -count=1 ./...

serve:
	templ generate --watch --cmd="go run main.go serve -p 4445"

clean:
	$(GOCLEAN)
	rm -f $(BINARY_NAME)

run: build
	./$(BINARY_NAME)

.PHONY: all build test clean sqlc templ init docs

sqlc:
	sqlc generate

templ:
	templ generate

init: sqlc templ
	go mod tidy

docs:
	mkdir -p docs/devlog
	go run main.go aiderdoc .aider.input.history docs/devlog/index.html

install-systemd:
	sudo cp systemd/*.service /etc/systemd/system/
	sudo systemctl daemon-reload
	@echo "Systemd service files installed. You can now enable and start the services with:"
	@echo "sudo systemctl enable jamie-listen jamie-transcribe jamie-serve"
	@echo "sudo systemctl start jamie-listen jamie-transcribe jamie-serve"

install: build
	sudo install -m 755 $(BINARY_NAME) /usr/local/bin/$(BINARY_NAME)
