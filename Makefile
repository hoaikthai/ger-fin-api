.PHONY: run build test lint swagger migrate docker-up docker-down clean

BINARY := ger_fin_api
MAIN   := cmd/main.go

run:
	go run $(MAIN)

build:
	go build -o bin/$(BINARY) $(MAIN)

test:
	go test ./...

test-verbose:
	go test -v ./...

test-real:
	go test -v -count=1 ./...

test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

lint:
	go vet ./...

swagger:
	swag init -g $(MAIN)

migrate:
	go run $(MAIN) --migrate-only

docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f

clean:
	rm -rf bin/ coverage.out coverage.html
