.PHONY: help setup wire

.DEFAULT_GOAL := help

help:
	@echo "Available options:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

setup: ## Install project dependencies
	go get -u github.com/google/wire/cmd/wire

wire: ## Generate wire_gen.go file
	cd internal && go generate
