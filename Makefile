.PHONY: build test run tidy fmt

build:
	go build -o shellia ./cmd/shellia

test:
	env GOCACHE=/tmp/go-build go test -count=1 ./...

run:
	go run ./cmd/shellia

tidy:
	go mod tidy

fmt:
	gofmt -w ./cmd ./internal
