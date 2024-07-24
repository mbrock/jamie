# Build the main application
build:
	go build -o jamie main.go

# Run the application with discord command
run: build
	./jamie discord

# Clean up build artifacts
clean:
	rm -f jamie

.PHONY: build run clean
