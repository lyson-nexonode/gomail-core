package jmap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"

	"github.com/lyson-nexonode/gomail-core/config"
	"github.com/lyson-nexonode/gomail-core/internal/ports"
)

// Server exposes the JMAP HTTP API (RFC 8620).
// It depends only on ports interfaces — never on concrete storage.
type Server struct {
	cfg            *config.Config
	log            *zap.Logger
	router         *chi.Mux
	mailboxReader  ports.MailboxReader
	messageReader  ports.MessageReader
	domainResolver ports.DomainResolver
	userAuth       ports.UserAuthenticator
}

// NewServer creates a new JMAP server and registers all routes.
func NewServer(
	cfg *config.Config,
	log *zap.Logger,
	mailboxReader ports.MailboxReader,
	messageReader ports.MessageReader,
	domainResolver ports.DomainResolver,
	userAuth ports.UserAuthenticator,
) *Server {
	s := &Server{
		cfg:            cfg,
		log:            log,
		mailboxReader:  mailboxReader,
		messageReader:  messageReader,
		domainResolver: domainResolver,
		userAuth:       userAuth,
	}

	s.router = chi.NewRouter()
	s.router.Use(middleware.RequestID)
	s.router.Use(middleware.Recoverer)
	s.router.Use(s.logMiddleware)

	// JMAP session endpoint — describes server capabilities (RFC 8620 section 2)
	s.router.Get("/.well-known/jmap", s.handleSession)

	// Authentication endpoint — returns a JWT token
	s.router.Post("/auth", s.handleAuth)

	// Main JMAP API endpoint — all method calls go here (RFC 8620 section 3)
	s.router.With(s.authMiddleware).Post("/jmap", s.handleJMAP)

	return s
}

// Start begins listening for HTTP connections.
func (s *Server) Start(ctx context.Context) error {
	srv := &http.Server{
		Addr:    s.cfg.JMAP.Addr,
		Handler: s.router,
	}

	s.log.Info("jmap server listening", zap.String("addr", s.cfg.JMAP.Addr))

	go func() {
		<-ctx.Done()
		s.log.Info("jmap server shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}

	return nil
}

// handleSession returns the JMAP session resource (RFC 8620 section 2).
// This is the entry point for JMAP clients — they discover capabilities here.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	// Extract user from token if present, otherwise return anonymous session
	var username string
	var accountID string

	tokenStr, err := ExtractBearerToken(r)
	if err == nil {
		claims, err := ValidateToken(tokenStr)
		if err == nil {
			username = claims.Username
			accountID = fmt.Sprintf("u%d", claims.UserID)
		}
	}

	if accountID == "" {
		accountID = "anonymous"
		username = "anonymous"
	}

	session := SessionResponse{
		Capabilities: map[string]interface{}{
			CapabilityCore: map[string]interface{}{
				"maxSizeUpload":         50 * 1024 * 1024,
				"maxConcurrentUpload":   4,
				"maxSizeRequest":        10 * 1024 * 1024,
				"maxConcurrentRequests": 4,
				"maxCallsInRequest":     16,
				"maxObjectsInGet":       500,
				"maxObjectsInSet":       500,
			},
			CapabilityMail: map[string]interface{}{},
		},
		Accounts: map[string]Account{
			accountID: {
				Name:       username,
				IsPersonal: true,
				Capabilities: map[string]interface{}{
					CapabilityMail: map[string]interface{}{},
				},
			},
		},
		PrimaryAccounts: map[string]string{
			CapabilityMail: accountID,
		},
		Username: username,
		APIURL:   fmt.Sprintf("http://%s/jmap", s.cfg.JMAP.Addr),
		State:    "state-v1",
	}

	s.writeJSON(w, http.StatusOK, session)
}

// handleAuth authenticates a user and returns a JWT token.
// This is a simplified auth endpoint for development.
// In production, use a proper OAuth2 / OIDC flow.
func (s *Server) handleAuth(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	localPart, domain, ok := parseAddress(body.Username)
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "Invalid credentials")
		return
	}

	ctx := r.Context()

	d, err := s.domainResolver.FindDomain(ctx, domain)
	if err != nil {
		s.writeError(w, http.StatusUnauthorized, "Invalid credentials")
		return
	}

	user, err := s.userAuth.FindUser(ctx, localPart, d.ID)
	if err != nil {
		s.writeError(w, http.StatusUnauthorized, "Invalid credentials")
		return
	}

	if !checkPassword(body.Password, user.PasswordHash) {
		s.writeError(w, http.StatusUnauthorized, "Invalid credentials")
		return
	}

	token, err := GenerateToken(user.ID, body.Username)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to generate token")
		return
	}

	s.log.Info("jmap auth successful", zap.String("username", body.Username))

	s.writeJSON(w, http.StatusOK, map[string]string{
		"token":     token,
		"accountId": fmt.Sprintf("u%d", user.ID),
	})
}

// handleJMAP processes a JMAP API request (RFC 8620 section 3).
// Each method call in the request is dispatched to the appropriate handler.
func (s *Server) handleJMAP(w http.ResponseWriter, r *http.Request) {
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	claims := r.Context().Value(contextKeyUser).(*Claims)

	resp := Response{
		SessionState: "state-v1",
	}

	for _, call := range req.MethodCalls {
		result := s.dispatch(r.Context(), claims, call)
		resp.MethodResponses = append(resp.MethodResponses, MethodResponse{
			Name:   result.Name,
			Result: result.Result,
			CallID: call.CallID,
		})
	}

	s.writeJSON(w, http.StatusOK, resp)
}

// dispatch routes a single JMAP method call to the appropriate handler.
func (s *Server) dispatch(ctx context.Context, claims *Claims, call MethodCall) MethodResponse {
	s.log.Debug("jmap dispatch",
		zap.String("method", call.Name),
		zap.String("call_id", call.CallID),
	)

	switch call.Name {
	case "Mailbox/get":
		return s.handleMailboxGet(ctx, claims, call)
	case "Mailbox/query":
		return s.handleMailboxQuery(ctx, claims, call)
	case "Email/get":
		return s.handleEmailGet(ctx, claims, call)
	case "Email/query":
		return s.handleEmailQuery(ctx, claims, call)
	default:
		return MethodResponse{
			Name: "error",
			Result: Error{
				Type:        "unknownMethod",
				Description: fmt.Sprintf("method %q is not supported", call.Name),
			},
			CallID: call.CallID,
		}
	}
}

// authMiddleware validates the Bearer token and injects claims into the context.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenStr, err := ExtractBearerToken(r)
		if err != nil {
			s.writeError(w, http.StatusUnauthorized, "Missing or invalid Authorization header")
			return
		}

		claims, err := ValidateToken(tokenStr)
		if err != nil {
			s.writeError(w, http.StatusUnauthorized, "Invalid or expired token")
			return
		}

		ctx := context.WithValue(r.Context(), contextKeyUser, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// logMiddleware logs every HTTP request with method, path and status.
func (s *Server) logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		s.log.Info("jmap request",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.Int("status", ww.Status()),
		)
	})
}

// writeJSON sends a JSON response with the given status code.
func (s *Server) writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.log.Error("jmap write json failed", zap.Error(err))
	}
}

// writeError sends a JSON error response.
func (s *Server) writeError(w http.ResponseWriter, status int, msg string) {
	s.writeJSON(w, status, map[string]string{"error": msg})
}

// contextKey is a private type for context keys to avoid collisions.
type contextKey string

const contextKeyUser contextKey = "user"

// parseAddress splits "user@domain" into local part and domain.
func parseAddress(addr string) (string, string, bool) {
	parts := strings.SplitN(addr, "@", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
