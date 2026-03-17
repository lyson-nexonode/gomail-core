# gomail-core

> A sovereign mail server written in Go — SMTP · IMAP · JMAP

[![GitHub CI](https://github.com/lyson-nexonode/gomail-core/actions/workflows/ci.yml/badge.svg)](https://github.com/lyson-nexonode/gomail-core/actions) [![GitLab CI](https://gitlab.com/lyson-nexonode/gomail-core/badges/main/pipeline.svg)](https://gitlab.com/lyson-nexonode/gomail-core/-/pipelines) [![Coverage](https://gitlab.com/lyson-nexonode/gomail-core/badges/main/coverage.svg)](https://gitlab.com/lyson-nexonode/gomail-core/-/commits/main)
[![Go Version](https://img.shields.io/badge/go-1.26+-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/lyson-nexonode/gomail-core)](https://goreportcard.com/report/github.com/lyson-nexonode/gomail-core)

Built as a technical demonstration of a production-grade mail server from scratch in Go, implementing SMTP (RFC 5321), IMAP4rev1 (RFC 3501) and JMAP (RFC 8620/8621) with clean architecture and FSM-based session management.

---

## Table of contents

- [Architecture](#architecture)
- [Stack](#stack)
- [Quick start](#quick-start)
- [Testing](#testing)
- [RFC Compliance](#rfc-compliance)
- [Design decisions](#design-decisions)
- [Project structure](#project-structure)
- [Roadmap](#roadmap)
- [Author](#author)

---

## Architecture

```
                    +------------------------------------------+
                    |              gomail-core                  |
                    |                                           |
  Mail clients ---> |  SMTP :2525   IMAP :1430   JMAP :8080    |
  JMAP clients ---> |                                           |
                    |          FSM session manager              |
                    |     (looplab/fsm — one FSM per conn)      |
                    |                                           |
                    |   ports.DeliveryPipeline                  |
                    |   ports.MailboxReader                     |
                    |   ports.MessageReader                     |
                    |   ports.DomainResolver                    |
                    |   ports.UserAuthenticator                 |
                    |                                           |
                    |   MySQL (metadata)   Redis (hot bodies)   |
                    +------------------------------------------+
```

The server follows a **ports & adapters (hexagonal) architecture**. Protocol handlers depend only on interfaces defined in `internal/ports` — never on concrete storage implementations. Domain events (`MessageReceived`, `MessageDelivered`) decouple the SMTP inbound pipeline from the storage layer.

### SMTP session FSM

```
INIT --> GREETED --> MAIL_FROM --> RCPT_TO --> DATA --> GREETED
                                    ^                      |
                                    |_______ RSET _________|
                         QUIT (valid from any state)
```

### IMAP session FSM (RFC 3501)

```
NOT_AUTHENTICATED --> AUTHENTICATED --> SELECTED --> AUTHENTICATED
                           |               |
                           +--- LOGOUT ----+
```

---

## Stack

| Component | Technology | Details |
|-----------|------------|---------|
| Language  | Go 1.26+   | Goroutines, channels, crypto/tls |
| SMTP      | Custom     | RFC 5321, FSM sessions, dot-unstuffing |
| IMAP      | Custom     | RFC 3501, FSM sessions, bcrypt auth |
| JMAP      | Custom     | RFC 8620/8621, JWT, HTTP/2 |
| Storage   | MySQL 8    | Metadata: users, mailboxes, messages |
| Cache     | Redis 7    | Hot message bodies, sessions, rate limits |
| FSM       | looplab/fsm | One FSM per TCP connection |
| Logging   | Uber Zap   | Structured JSON, colored dev output |
| HTTP      | chi router | JMAP API, middleware chain |
| Auth      | bcrypt + JWT | IMAP plain auth, JMAP Bearer token |
| Profiling | pprof      | Internal port only — never public |
| Infra     | Docker Compose / Kubernetes-ready | |

---

## Quick start

**Requirements**: Go 1.26+, Docker, Docker Compose

```bash
git clone https://github.com/lyson-nexonode/gomail-core.git
cd gomail-core

# Start MySQL and Redis
docker compose up -d mysql redis

# Apply SQL migrations
make migrate

# Run all servers
go run ./cmd/smtp/   # Terminal 1 — port 2525
go run ./cmd/imap/   # Terminal 2 — port 1430
go run ./cmd/jmap/   # Terminal 3 — port 8080
```

### Send a test email via SMTP

```bash
telnet localhost 2525

EHLO testclient
MAIL FROM:<alice@gomail.local>
RCPT TO:<test@gomail.local>
DATA
From: alice@gomail.local
To: test@gomail.local
Subject: Hello gomail-core

First email on a sovereign mail server.
.
QUIT
```

### Read emails via IMAP

```bash
telnet localhost 1430

A001 LOGIN test@gomail.local password
A002 LIST "" "*"
A003 SELECT INBOX
A004 FETCH 1 (FLAGS UID RFC822.SIZE BODY[])
A005 LOGOUT
```

### Authenticate and query via JMAP

```bash
# Get a JWT token
TOKEN=$(curl -s -X POST http://localhost:8080/auth \
  -H "Content-Type: application/json" \
  -d '{"username":"test@gomail.local","password":"password"}' | jq -r .token)

# List mailboxes and emails
curl -s -X POST http://localhost:8080/jmap \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "using": ["urn:ietf:params:jmap:core","urn:ietf:params:jmap:mail"],
    "methodCalls": [
      ["Mailbox/get", {"accountId": "u1"}, "c1"],
      ["Email/query", {"accountId": "u1"}, "c2"],
      ["Email/get",   {"accountId": "u1"}, "c3"]
    ]
  }' | jq .
```

---

## Testing

```bash
# Run all tests with race detector
go test ./... -v -race

# Coverage report
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out

# Run a specific package
go test ./internal/smtp/... -v
go test ./internal/imap/... -v
go test ./internal/jmap/... -v
```

### What is tested

- **SMTP FSM** — all valid transitions, all invalid transitions, RSET, full transaction cycle
- **IMAP FSM** — all valid transitions, all invalid transitions, re-select handling
- **JMAP encoding** — MethodCall deserialization, MethodResponse serialization, full request parsing
- **JMAP auth** — JWT generate/validate, expired token rejection, tampered token rejection, Bearer extraction
- **Parsers** — extractAddress (angle brackets, empty address, prefix matching), parseAddress, extractSubject
- **Auth** — bcrypt checkPassword (correct, wrong, empty, invalid hash)

---

## RFC Compliance

### SMTP — RFC 5321

| Command | Status | Notes |
|---------|--------|-------|
| EHLO / HELO | Implemented | Advertises SIZE, 8BITMIME, ENHANCEDSTATUSCODES |
| MAIL FROM | Implemented | Validates address format |
| RCPT TO | Implemented | Multiple recipients, limit 100 |
| DATA | Implemented | Dot-unstuffing, configurable size limit (25 MB default) |
| RSET | Implemented | Resets transaction, keeps session alive |
| NOOP | Implemented | RFC 5321 section 4.1.1.9 |
| QUIT | Implemented | Graceful session close |
| STARTTLS | Roadmap | TLS 1.3 planned |
| AUTH PLAIN | Roadmap | SASL authentication planned |
| SIZE extension | Implemented | Advertised in EHLO response |

### IMAP — RFC 3501

| Command | Status | Notes |
|---------|--------|-------|
| CAPABILITY | Implemented | IMAP4rev1, LITERAL+, SASL-IR, LOGIN, IDLE |
| LOGIN | Implemented | bcrypt password verification |
| SELECT | Implemented | EXISTS, RECENT, UIDVALIDITY, UIDNEXT, FLAGS |
| EXAMINE | Implemented | Read-only SELECT |
| LIST | Implemented | Pattern matching (* and %) |
| LSUB | Implemented | Same as LIST for now |
| FETCH | Implemented | FLAGS, UID, RFC822.SIZE, BODY[], INTERNALDATE |
| STORE | Implemented | Flag updates — MySQL persistence roadmap |
| SEARCH | Implemented | Full scan — query optimisation roadmap |
| EXPUNGE | Implemented | Deletion — MySQL implementation roadmap |
| CLOSE | Implemented | Returns to Authenticated state |
| LOGOUT | Implemented | Valid from any state |
| NOOP | Implemented | |
| IDLE | Roadmap | Push notifications for new mail |
| STARTTLS | Roadmap | TLS 1.3 planned |

### JMAP — RFC 8620 / RFC 8621

| Method | Status | Notes |
|--------|--------|-------|
| Session endpoint | Implemented | /.well-known/jmap |
| JWT Authentication | Implemented | POST /auth — HS256 signed tokens |
| Mailbox/get | Implemented | Returns all mailboxes with roles |
| Mailbox/query | Implemented | Returns mailbox IDs |
| Email/get | Implemented | Full email list with metadata |
| Email/query | Implemented | Returns email IDs |
| Email/set | Roadmap | Create, update, destroy |
| Thread/get | Roadmap | Email threading |
| Identity/get | Roadmap | Sender identities |
| Push notifications | Roadmap | EventSource push |
| JMAP over WebSocket | Roadmap | RFC 8887 |

---

## Design decisions

### FSM-based session management

SMTP and IMAP sessions are modeled as finite state machines using `looplab/fsm`. This enforces RFC-compliant command sequencing at the type level — a `DATA` command sent before `RCPT TO` is rejected by the FSM before any handler runs. Invalid transitions are logged and return `503 Bad sequence of commands`.

The pattern is directly inspired by a generic FSM framework (GO-FSMX) built for industrial robotics orchestration at scale (21 production sites, ~170 autonomous robots), adapted here for mail protocol sessions.

### MySQL + Redis split storage

Message metadata (envelope, flags, UIDs, subjects, mailbox assignments) is stored permanently in MySQL. Raw message bodies are cached in Redis with a 24h TTL for fast IMAP FETCH access. On cache miss, the server falls back to the `raw_key` stored in MySQL for retrieval from permanent storage.

This separation keeps MySQL queries lean (no large blobs) and IMAP FETCH responses fast (Redis reads in microseconds vs. MySQL reads from disk).

### Ports & adapters (hexagonal architecture)

Protocol handlers (`internal/smtp`, `internal/imap`, `internal/jmap`) depend only on interfaces defined in `internal/ports`:

- `DeliveryPipeline` — used by SMTP to hand off received messages
- `MailboxReader` — used by IMAP and JMAP to list and find mailboxes
- `MessageReader` — used by IMAP FETCH and JMAP Email/get
- `DomainResolver` — used by SMTP and IMAP to validate recipient domains
- `UserAuthenticator` — used by IMAP LOGIN and JMAP /auth

The `storage.MessageStore` struct implements all five interfaces. Swapping MySQL for PostgreSQL or adding a secondary cache requires zero changes to the protocol handlers.

### JMAP alongside IMAP

JMAP (RFC 8620/8621) is the IETF-standardized replacement for IMAP. Published in 2019, it uses HTTP/2, JSON, and delta synchronization — clients request only what changed since a known state, eliminating the full-mailbox polling that makes IMAP expensive at scale. Implementing JMAP positions gomail-core as forward-compatible with next-generation mail clients while maintaining IMAP compatibility for existing ones.

### UID allocation with SELECT FOR UPDATE

IMAP UIDs must be monotonically increasing and globally unique per mailbox (RFC 3501 section 2.3.1.1). The server uses a MySQL transaction with `SELECT ... FOR UPDATE` on the mailbox row to atomically read and increment `uid_next`. This prevents UID collisions under concurrent delivery without requiring application-level locking.

### Event-driven delivery pipeline

When the SMTP server completes a DATA transfer, it publishes a `MessageReceived` event to the `DeliveryPipeline` port. The SMTP session knows nothing about MySQL, Redis, or any storage detail — it only calls `delivery.Deliver(ctx, event)`. The `MessageStore` adapter receives the event and orchestrates persistence: MySQL insert first (source of truth), then Redis cache (performance optimization, non-fatal if it fails).

---

## Project structure

```
gomail-core/
├── cmd/
│   ├── smtp/               # SMTP server entrypoint
│   ├── imap/               # IMAP server entrypoint
│   └── jmap/               # JMAP server entrypoint
├── config/
│   └── config.go           # Environment-based configuration with defaults
├── internal/
│   ├── ports/
│   │   ├── storage.go      # Interface definitions (DeliveryPipeline, MailboxReader...)
│   │   └── events.go       # Domain events (MessageReceived, MessageDelivered)
│   ├── smtp/
│   │   ├── server.go       # TCP listener, goroutine per connection
│   │   ├── session.go      # FSM definition and session lifecycle
│   │   ├── handler.go      # SMTP command handlers
│   │   └── envelope.go     # Mail envelope (from, to, body)
│   ├── imap/
│   │   ├── server.go       # TCP listener, goroutine per connection
│   │   ├── session.go      # FSM definition and session lifecycle
│   │   ├── handler.go      # IMAP command handlers
│   │   ├── auth.go         # bcrypt password verification
│   │   └── types.go        # IMAP types (flags, fetch items, selected mailbox)
│   ├── jmap/
│   │   ├── server.go       # HTTP server, middleware chain, dispatcher
│   │   ├── methods.go      # Mailbox/get, Email/get, Email/query...
│   │   ├── auth.go         # JWT generate/validate, Bearer extraction
│   │   ├── encoding.go     # Custom JSON marshaling for MethodCall/Response
│   │   ├── password.go     # bcrypt helper
│   │   └── types.go        # JMAP types (Request, Response, JMAPEmail...)
│   ├── storage/
│   │   ├── message_store.go # Implements all ports — orchestrates MySQL + Redis
│   │   ├── mysql/
│   │   │   ├── store.go     # Connection pool, DSN masking
│   │   │   ├── domain.go    # Domain queries
│   │   │   ├── user.go      # User queries
│   │   │   ├── mailbox.go   # Mailbox queries + UID allocation
│   │   │   └── message.go   # Message insert and retrieval
│   │   └── redis/
│   │       └── store.go     # Message body cache, rate limiting
│   └── telemetry/
│       ├── logger.go        # Zap logger (dev: colored, prod: JSON)
│       └── pprof.go         # pprof endpoint (localhost only)
├── migrations/
│   └── 001_init.sql         # Schema: domains, users, mailboxes, messages
├── docker-compose.yml        # MySQL 8 + Redis 7 with healthchecks
├── Makefile                  # up, build, test, lint, migrate
└── go.mod
```

---

## Roadmap

**Security**
- TLS 1.3 — STARTTLS on SMTP port 587, IMAPS on port 993
- DKIM — outbound email signing (RFC 6376)
- SPF — inbound sender validation (RFC 7208)
- DMARC — policy enforcement (RFC 7489)
- Encryption at rest — AES-256-GCM message body encryption

**Protocol completeness**
- IMAP IDLE — server-push notifications for new mail (RFC 2177)
- Email/set — JMAP email creation, update and destruction
- Thread/get — JMAP email threading
- JMAP Push — EventSource notifications (RFC 8620 section 7)
- SMTP AUTH PLAIN — SASL authentication

**Storage & scalability**
- Flag persistence — IMAP STORE writes flags to MySQL
- Quota enforcement — per-user storage limits with 4xx rejection
- Multi-domain alias support — alias_of field on domains table
- Clustering — MySQL Vitess sharding for millions of mailboxes
- SMTP outbound queue — Redis-based delivery queue with exponential backoff

**Observability**
- Prometheus metrics — smtp_received_total, imap_connections_active, jmap_requests_total
- Grafana dashboard
- Distributed tracing — OpenTelemetry

**Deployment**
- Kubernetes Helm chart
- Let's Encrypt — automatic TLS certificate provisioning
- GitLab CI pipeline — lint, test, build, docker push

---

## Author

**Ciré LY** — Senior Software Engineer, distributed systems & Go

7+ years building production-critical backend platforms, event-driven architectures and FSM-based workflow orchestration at scale.

[linkedin.com/in/cirehamathly](https://linkedin.com/in/cirehamathly) · cire.ly@nexonode.tech
