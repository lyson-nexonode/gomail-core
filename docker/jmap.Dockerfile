# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/jmap ./cmd/jmap

# Runtime stage
FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

COPY --from=builder /bin/jmap /app/jmap

RUN addgroup -S gomail && adduser -S gomail -G gomail
USER gomail

EXPOSE 8080 6063

CMD ["/app/jmap"]
