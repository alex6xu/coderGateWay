FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -o codegateway ./cmd/server/

# Final stage
FROM alpine:latest

WORKDIR /app

# Install ca-certificates for HTTPS
RUN apk add --no-cache ca-certificates

# Copy binary
COPY --from=builder /app/codegateway .

# Copy config
COPY codegateway.yaml .

# Create data directory
RUN mkdir -p /app/data

# Expose port
EXPOSE 8080

# Run
CMD ["./codegateway"]
