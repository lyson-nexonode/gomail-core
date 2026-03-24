.PHONY: up down logs build test lint migrate

# Start only storage services for local development
up:
	docker compose up -d mysql redis

down:
	docker compose down

logs:
	docker compose logs -f

# Build individual server binaries
build-smtp:
	go build -o bin/smtp ./cmd/smtp

build-imap:
	go build -o bin/imap ./cmd/imap

build-jmap:
	go build -o bin/jmap ./cmd/jmap

build: build-smtp build-imap build-jmap

# Run tests with race detector enabled
test:
	CGO_ENABLED=1 go test ./... -v -race

test-cover:
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out

lint:
	golangci-lint run ./...

# Apply SQL migrations against the running MySQL container
migrate:
	docker compose exec mysql mysql -u gomail -pgomailpassword gomail < migrations/001_init.sql

# Hot reload for development using air
dev-smtp:
	air -c .air.smtp.toml

dev-imap:
	air -c .air.imap.toml
