# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY cmd/ ./cmd/
COPY database/ ./database/
COPY pkg/ ./pkg/
COPY migrations/ ./migrations/

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -o bot ./cmd/

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Create data directory for database
RUN mkdir -p /data

# Copy binary and migrations
COPY --from=builder /app/bot .
COPY --from=builder /app/migrations ./migrations/

CMD ["./bot"]
