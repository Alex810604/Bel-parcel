FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /operator-api ./cmd/operator-api/main.go

FROM alpine:3.20
WORKDIR /app
COPY --from=builder /operator-api /app/operator-api
EXPOSE 8090
RUN adduser -D -g '' appuser
USER appuser
ENTRYPOINT ["/app/operator-api"]
