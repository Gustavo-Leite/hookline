COMPOSE_DEV = docker compose -f docker-compose.yml -f docker-compose.dev.yml

.PHONY: dev up down logs test lint fmt tidy migrate-up migrate-down migrate-status admin

dev:
	$(COMPOSE_DEV) up -d --build
	$(COMPOSE_DEV) logs -f api

up:
	docker compose up -d --build

down:
	docker compose down

logs:
	docker compose logs -f api

admin:
	docker compose run --rm admin create-application "$(name)"

test:
	go test -race ./...

lint:
	golangci-lint run

fmt:
	golangci-lint fmt

tidy:
	go mod tidy

migrate-up:
	goose up

migrate-down:
	goose down

migrate-status:
	goose status
