.PHONY: setup
setup:
	go get -u github.com/google/wire/cmd/wire

.PHONY: wire
wire:
	cd internal && go generate

# .PHONY is used for reserving tasks words
