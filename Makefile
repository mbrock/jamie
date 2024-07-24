# Build the main application
build:
	go build -o jamie main.go

# Run the application with discord command
run: build
	./jamie discord

# Run migrations and prepare statements
migrate:
	go run cmd/migrate/main.go

# Clean up build artifacts
clean:
	rm -f jamie

.PHONY: build run migrate clean
