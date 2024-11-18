# Simple Makefile for a Go project
help: ## Show available options
	@echo "Available options:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
# Build the application
all: ## Build and test the application
	@make build
	@make test

build: ## Build the application
	@echo "Building..."


	@go build -o main cmd/api/main.go

# Run the application
run: ## Run the application
	@go run cmd/api/main.go

# Test the application
test: ## Test the application
	@echo "Testing..."
	@go test ./... -v

# Clean the binary
clean: ## Clean the binary
	@echo "Cleaning..."
	@rm -f main

# Live Reload
watch: ## Watch the application
	@if command -v air > /dev/null; then \
            air; \
            echo "Watching...";\
        else \
            read -p "Go's 'air' is not installed on your machine. Do you want to install it? [Y/n] " choice; \
            if [ "$$choice" != "n" ] && [ "$$choice" != "N" ]; then \
                go install github.com/air-verse/air@latest; \
                air; \
                echo "Watching...";\
            else \
                echo "You chose not to install air. Exiting..."; \
                exit 1; \
            fi; \
        fi

init-db: ## Initialize the database
	sqlite3 ./sqlite.db < ./internal/database/schema.sql

wire: ## Generate the wire dependencies
	wire ./internal/wire

.PHONY: help all build run test clean watch init-db wire
