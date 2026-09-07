COMPOSE_DEV = docker compose -f docker-compose.yml -f docker-compose.dev.yml

.PHONY: setup dev up down logs test test-unit cover lint vuln fmt tidy migrate-up migrate-down migrate-status admin loadtest

setup:
	@test -f .env && echo ".env already exists, leaving it alone" || ( \
		sed "s|^SECRET_ENCRYPTION_KEY=.*|SECRET_ENCRYPTION_KEY=$$(openssl rand -base64 32)|" .env.example > .env && \
		echo "created .env with a freshly generated encryption key" )

dev:
	$(COMPOSE_DEV) up -d --build
	$(COMPOSE_DEV) logs -f api worker

up:
	docker compose up -d --build

down:
	docker compose down

logs:
	docker compose logs -f api worker

admin:
	docker compose run --rm admin create-application "$(name)"

loadtest:
	docker run --rm -i --network host \
		-e HOOKLINE_KEY="$(key)" \
		grafana/k6:1.4.1 run - < deploy/k6/ingest.js

test:
	go test -race ./...

test-unit:
	go test -race -short ./...

cover:
	go test ./... -coverprofile=coverage.out
	go tool cover -func=coverage.out | tail -1

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

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
