FUZZTIME ?= 10s
FUZZ_WORKERS ?= 2

.PHONY: format lint fuzz-fit fuzz-large-inputs fuzz-all

format:
	go tool gofumpt -w .

lint:
	go tool golangci-lint run ./...

fuzz-fit:
	go test -run='^$$' -fuzz='^FuzzFitComplexity$$' -fuzztime=$(FUZZTIME) -parallel=$(FUZZ_WORKERS)

fuzz-large-inputs:
	go test -run='^$$' -fuzz='^FuzzComplexityLargeInputs$$' -fuzztime=$(FUZZTIME) -parallel=$(FUZZ_WORKERS)

fuzz-all:
	$(MAKE) fuzz-fit FUZZTIME=$(FUZZTIME) FUZZ_WORKERS=$(FUZZ_WORKERS)
	$(MAKE) fuzz-large-inputs FUZZTIME=$(FUZZTIME) FUZZ_WORKERS=$(FUZZ_WORKERS)
