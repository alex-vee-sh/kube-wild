BIN := kubectl-wild
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

.PHONY: build
build:
	GO111MODULE=on go build -o ./$(BIN) .

.PHONY: clean
clean:
	rm -f ./$(BIN)

.PHONY: test
test:
	GO111MODULE=on go test ./...


