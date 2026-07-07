.PHONY: build clean test run docker-build docker-run help

# Default target
all: build

# Build the application
build:
	@echo "Building RTMP server..."
	go build -o rtmp-server cmd/server/main.go

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -f rtmp-server
	rm -rf hls/
	rm -rf bin/
	rm -rf dist/

# Run tests
test:
	@echo "Running tests..."
	go test ./...

# Run the server
run: build
	@echo "Starting RTMP server..."
	./rtmp-server -config config.yaml

# Build Docker image
docker-build:
	@echo "Building Docker image..."
	docker build -t rtmp-server .

# Run with Docker Compose
docker-run:
	@echo "Starting RTMP server with Docker Compose..."
	docker-compose up --build

# Stop Docker Compose
docker-stop:
	@echo "Stopping Docker Compose..."
	docker-compose down

# Install dependencies
deps:
	@echo "Installing dependencies..."
	go mod download
	go mod tidy

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Lint code
lint:
	@echo "Linting code..."
	golangci-lint run

# Generate test coverage
coverage:
	@echo "Generating test coverage..."
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

# Show help
help:
	@echo "Available targets:"
	@echo "  build        - Build the RTMP server"
	@echo "  clean        - Clean build artifacts"
	@echo "  test         - Run tests"
	@echo "  run          - Build and run the server"
	@echo "  docker-build - Build Docker image"
	@echo "  docker-run   - Run with Docker Compose"
	@echo "  docker-stop  - Stop Docker Compose"
	@echo "  deps         - Install dependencies"
	@echo "  fmt          - Format code"
	@echo "  lint         - Lint code"
	@echo "  coverage     - Generate test coverage"
	@echo "  help         - Show this help" 