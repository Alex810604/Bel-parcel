FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /reference-service ./cmd/reference-service/main.go

FROM alpine:3.20
WORKDIR /app
COPY --from=builder /reference-service /app/reference-service
EXPOSE 8084
RUN adduser -D -g '' appuser
USER appuser
ENTRYPOINT ["/app/reference-service"]
