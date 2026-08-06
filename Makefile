.PHONY: key build up down logs health health-deep test tidy \
        migrate-up migrate-down migrate-version migrate-force

#setup
key:
	go run scripts/keygen/main.go

tidy:
	go mod tidy

# docker
build:
	docker compose build --no-cache

up:
	docker compose up -d

stop:
	docker compose stop

down:
	docker compose down

down-volumes:
	docker compose down -v

logs:
	docker compose logs -f openstore

logs-all:
	docker compose logs -f

ps:
	docker compose ps

#health
health:
	curl -s http://localhost:8080/health | jq .

health-deep:
	curl -s http://localhost:8080/health/deep | jq .

#migrations
migrate-up:
	go run scripts/migrate/main.go up

migrate-down:
	@read -p "How many steps to roll back? " n; \
	go run scripts/migrate/main.go down $$n

migrate-version:
	go run scripts/migrate/main.go version

migrate-force:
	@read -p "Force to version? " n; \
	go run scripts/migrate/main.go force $$n

#tests
test:
	go test -race ./...

test-verbose:
	go test -race -v ./...

test-cover:
	go test -race ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out

test-unit:
	go test -race ./internal/security/... ./internal/quota/... ./internal/webhook/... ./internal/seaweedfs/...

test-integration:
	go test -race ./internal/handlers/...

test-e2e:
	go test -race -v ./tests/e2e/... -tags e2e

#build binary locally
bin:
	go build -o openstore ./cmd/openstore

clean:
	rm -f openstore coverage.out
