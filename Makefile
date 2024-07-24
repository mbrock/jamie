# Build the main application
build:
	go build -o jamie main.go

# Run the application
run: build
	./jamie

# Clean up build artifacts
clean:
	rm -f jamie

.PHONY: build run clean
