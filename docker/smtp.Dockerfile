# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Download dependencies first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/smtp ./cmd/smtp

# Runtime stage — minimal image
FROM alpine:3.19

# Add CA certificates for TLS and timezone data
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

COPY --from=builder /bin/smtp /app/smtp

# Never run as root in production
RUN addgroup -S gomail && adduser -S gomail -G gomail
USER gomail

EXPOSE 2525 5870 6061

CMD ["/app/smtp"]
