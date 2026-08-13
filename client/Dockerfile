# Build stage
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod .
RUN go mod download
COPY client/ ./client/
RUN CGO_ENABLED=0 GOOS=linux go build -o /client-app ./client/

# Runtime stage: alpine para ter shell disponível (facilita debugging)
FROM alpine:3.19
COPY --from=builder /client-app /client-app
ENTRYPOINT ["/client-app"]
