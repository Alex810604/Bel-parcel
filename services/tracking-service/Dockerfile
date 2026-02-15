FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /tracking-service ./cmd/tracking-service/main.go

FROM alpine:3.20
WORKDIR /app
COPY --from=builder /tracking-service /app/tracking-service
EXPOSE 8087
RUN adduser -D -g '' appuser
USER appuser
ENTRYPOINT ["/app/tracking-service"]
