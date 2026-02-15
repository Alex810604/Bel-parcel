# syntax=docker/dockerfile:1

FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /routing-service ./cmd/routing-service/main.go

FROM alpine:3.20
WORKDIR /app
COPY --from=builder /routing-service /app/routing-service
EXPOSE 8081
RUN adduser -D -g '' appuser
USER appuser
ENTRYPOINT ["/app/routing-service"]

