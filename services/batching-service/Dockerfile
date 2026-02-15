FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /batching-service ./cmd/batching-service/main.go

FROM alpine:3.20
WORKDIR /app
COPY --from=builder /batching-service /app/batching-service
EXPOSE 8083
RUN adduser -D -g '' appuser
USER appuser
ENTRYPOINT ["/app/batching-service"]
