# Build the main application
build:
	go build -o jamie main.go

# Run the application with discord command
run: build
	./jamie discord

# Run the application in debug mode
debug: build
	LOG_LEVEL=debug ./jamie discord

# Run migrations and prepare statements
migrate:
	go run cmd/migrate/main.go

# Clean up build artifacts
clean:
	rm -f jamie

# Drop the database
drop:
	rm -f 001.db

.PHONY: build run migrate clean drop
