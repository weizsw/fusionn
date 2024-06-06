setup:
	go get -u github.com/google/wire/cmd/wire

wire:
	cd internal && go generate

# .PHONY is used for reserving tasks words
.PHONY: setup start build wire