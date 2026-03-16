# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/imap ./cmd/imap

# Runtime stage
FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

COPY --from=builder /bin/imap /app/imap

RUN addgroup -S gomail && adduser -S gomail -G gomail
USER gomail

EXPOSE 1430 9930 6062

CMD ["/app/imap"]
