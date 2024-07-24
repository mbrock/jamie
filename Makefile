# Build the main application
build:
	go build -gcflags="all=-N -l" -o jamie main.go

# Run the application with discord command
run: build
	./jamie discord

# Run the application in debug mode
debug: build
	LOG_LEVEL=debug ./jamie discord

# Run migrations and prepare statements
migrate:
	go run cmd/migrate/main.go

# Build with race detection
build-race:
	go build -race -o jamie-race main.go

# Clean up build artifacts
clean:
	rm -f jamie jamie-race

# Drop the database
drop:
	rm -f 001.db

.PHONY: build run migrate clean drop
