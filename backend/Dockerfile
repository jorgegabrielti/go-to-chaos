# Build stage: compila o binário estático
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod .
RUN go mod download
COPY backend/ ./backend/
RUN CGO_ENABLED=0 GOOS=linux go build -o /backend-server ./backend/

# Runtime stage: alpine para ter wget disponível (necessário para healthcheck)
FROM alpine:3.19
RUN apk --no-cache add wget
COPY --from=builder /backend-server /backend-server
EXPOSE 3000
ENTRYPOINT ["/backend-server"]
