FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /mobile-gateway ./cmd/mobile-gateway/main.go

FROM alpine:3.20
WORKDIR /app
COPY --from=builder /mobile-gateway /app/mobile-gateway
EXPOSE 8088
RUN adduser -D -g '' appuser
USER appuser
ENTRYPOINT ["/app/mobile-gateway"]
