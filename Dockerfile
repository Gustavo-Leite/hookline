# syntax=docker/dockerfile:1

# ---------- build the service ----------
FROM golang:1.27-alpine AS build

WORKDIR /src

# Dependencies change less often than code, so download them in their own layer.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/hookline-admin ./cmd/hookline-admin

# ---------- development: hot reload, source comes from a bind mount ----------
FROM golang:1.27-alpine AS dev

WORKDIR /src

RUN go install github.com/air-verse/air@v1.67.4

COPY go.mod go.sum ./
RUN go mod download

EXPOSE 8080
CMD ["air", "-c", ".air.toml"]

# ---------- build the migration runner ----------
FROM golang:1.27-alpine AS goose-build

RUN go install github.com/pressly/goose/v3/cmd/goose@v3.28.0

# ---------- final: the API ----------
FROM gcr.io/distroless/static-debian12:nonroot AS api

COPY --from=build /out/api /api

EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/api"]

# ---------- final: the admin CLI ----------
FROM gcr.io/distroless/static-debian12:nonroot AS admin

COPY --from=build /out/hookline-admin /hookline-admin

USER nonroot:nonroot
ENTRYPOINT ["/hookline-admin"]

# ---------- final: one-shot migrations ----------
FROM alpine:3.22 AS migrate

COPY --from=goose-build /go/bin/goose /usr/local/bin/goose
COPY migrations/ /migrations/

WORKDIR /migrations
ENTRYPOINT ["goose"]
CMD ["up"]
