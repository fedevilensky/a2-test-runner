FUZZTIME ?= 10s
FUZZ_WORKERS ?= 2

.PHONY: fuzz-fit fuzz-large-inputs fuzz-all

fuzz-fit:
	go test -run='^$$' -fuzz='^FuzzFitComplexity$$' -fuzztime=$(FUZZTIME) -parallel=$(FUZZ_WORKERS)

fuzz-large-inputs:
	go test -run='^$$' -fuzz='^FuzzComplexityLargeInputs$$' -fuzztime=$(FUZZTIME) -parallel=$(FUZZ_WORKERS)

fuzz-all:
	$(MAKE) fuzz-fit FUZZTIME=$(FUZZTIME) FUZZ_WORKERS=$(FUZZ_WORKERS)
	$(MAKE) fuzz-large-inputs FUZZTIME=$(FUZZTIME) FUZZ_WORKERS=$(FUZZ_WORKERS)
