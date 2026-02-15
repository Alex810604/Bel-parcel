# syntax=docker/dockerfile:1

# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /reassignment-service ./cmd/reassignment-service/main.go

# Final stage
FROM alpine:3.20

WORKDIR /app

COPY --from=builder /reassignment-service /app/reassignment-service

# Expose port
EXPOSE 8082

# Create a non-root user
RUN adduser -D -g '' appuser
USER appuser

ENTRYPOINT ["/app/reassignment-service"]
