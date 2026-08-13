# Build stage
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod .
RUN go mod download
COPY proxy/ ./proxy/
RUN CGO_ENABLED=0 GOOS=linux go build -o /proxy-server ./proxy/

# Runtime stage: alpine para ter shell disponível (facilita debugging)
FROM alpine:3.19
COPY --from=builder /proxy-server /proxy-server
EXPOSE 8080 8081
ENTRYPOINT ["/proxy-server"]
