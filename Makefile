.PHONY: build run test test-race test-pg vet fmt fmt-check cover tidy clean

build:
	go build -o bin/exotel-call-service ./cmd/server

run:
	go run ./cmd/server

test:
	go test -count=1 ./...

test-race:
	go test -race -count=1 ./...

# Run the suite against a real Postgres (set TEST_DATABASE_URL, e.g. a local
# docker: docker run --rm -e POSTGRES_PASSWORD=postgres -p 5432:5432 postgres:16).
test-pg:
	TEST_DATABASE_URL?=postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable \
	go test -count=1 ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "not gofmt'd:"; echo "$$out"; exit 1; fi

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

tidy:
	go mod tidy

clean:
	rm -rf bin coverage.out
