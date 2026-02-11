BINARY := server-price-tracker
CMD    := ./cmd/server-price-tracker

.PHONY: build run test test-integration test-coverage lint fmt migrate mocks clean

build:
	go build -o bin/$(BINARY) $(CMD)

run:
	go run $(CMD) serve

test:
	go test ./...

test-integration:
	go test -tags integration -count=1 ./...

test-coverage:
	go test -race -coverprofile=coverage.out -covermode=atomic ./...

lint:
	golangci-lint run ./...

fmt:
	goimports -w .
	golines -w .

migrate:
	go run $(CMD) migrate

mocks:
	mockery

clean:
	rm -rf bin/ coverage.out
