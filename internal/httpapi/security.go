package httpapi

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	DisplayName  string `json:"display_name"`
	Role         string `json:"role"`
	PasswordHash string `json:"-"`
	MFASecret    string `json:"-"`
	MFAEnabled   bool   `json:"mfa_enabled"`
}
type Session struct {
	TokenHash  string    `json:"-"`
	UserID     int64     `json:"user_id"`
	CSRFToken  string    `json:"csrf_token,omitempty"`
	UserAgent  string    `json:"user_agent"`
	IPAddress  string    `json:"ip_address"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}
type AuthStore interface {
	EnsureAdmin(context.Context, string, string, string) error
	UserByEmail(context.Context, string) (User, error)
	UserByID(context.Context, int64) (User, error)
	ListUsers(context.Context) ([]User, error)
	CreateUser(context.Context, string, string, string, string) (User, error)
	UpdatePassword(context.Context, int64, string) error
	UpdateMFA(context.Context, int64, string, bool) error
	CreateSession(context.Context, Session) error
	SessionByHash(context.Context, string) (Session, error)
	TouchSession(context.Context, string) error
	DeleteSession(context.Context, string) error
	DeleteUserSessions(context.Context, int64, string) error
	ListSessions(context.Context, int64) ([]Session, error)
	ReplaceRecoveryCodes(context.Context, int64, []string) error
	UseRecoveryCode(context.Context, int64, string) (bool, error)
}

type authContextKey struct{}
type authContext struct {
	User    User
	Session Session
}

func SecureHandler(revision string, store DeviceStore, auth AuthStore, webRoot string, forwarders ...TelemetryForwarder) http.Handler {
	return SecureHandlerWithRuntime(revision, store, auth, webRoot, nil, forwarders...)
}
func SecureHandlerWithRuntime(revision string, store DeviceStore, auth AuthStore, webRoot string, runtime *SimulationRuntime, forwarders ...TelemetryForwarder) http.Handler {
	core := HandlerWithRuntime(revision, store, webRoot, runtime, forwarders...)
	if auth == nil {
		return core
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Username, Email, Password, Code string }
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			writeError(w, 400, "invalid login payload")
			return
		}
		username := in.Username
		if username == "" {
			username = in.Email // Backward compatibility for existing API clients.
		}
		u, err := auth.UserByEmail(r.Context(), strings.ToLower(strings.TrimSpace(username)))
		if err != nil || !verifyPassword(u.PasswordHash, in.Password) {
			writeError(w, 401, "invalid username or password")
			return
		}
		if u.MFAEnabled && strings.TrimSpace(in.Code) == "" {
			writeJSON(w, http.StatusAccepted, map[string]any{"mfa_required": true})
			return
		}
		if u.MFAEnabled && !verifyTOTP(decryptSecret(u.MFASecret), in.Code, time.Now()) {
			ok, _ := auth.UseRecoveryCode(r.Context(), u.ID, hashText(strings.ToUpper(strings.TrimSpace(in.Code))))
			if !ok {
				writeError(w, 401, "valid authenticator or recovery code required")
				return
			}
		}
		raw := randomToken(32)
		csrf := randomToken(24)
		s := Session{TokenHash: hashText(raw), UserID: u.ID, CSRFToken: csrf, UserAgent: r.UserAgent(), IPAddress: remoteIP(r), CreatedAt: time.Now(), LastSeenAt: time.Now(), ExpiresAt: time.Now().Add(12 * time.Hour)}
		if auth.CreateSession(r.Context(), s) != nil {
			writeError(w, 500, "create session")
			return
		}
		setSessionCookie(w, raw)
		writeJSON(w, 200, map[string]any{"user": u, "csrf_token": csrf, "mfa_setup_required": !u.MFAEnabled})
	})
	mux.HandleFunc("POST /api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if s, ok := authenticate(r, auth); ok {
			_ = auth.DeleteSession(r.Context(), s.Session.TokenHash)
		}
		clearSessionCookie(w)
		w.WriteHeader(204)
	})
	protected := http.NewServeMux()
	protected.HandleFunc("GET /api/auth/me", func(w http.ResponseWriter, r *http.Request) {
		a := r.Context().Value(authContextKey{}).(authContext)
		writeJSON(w, 200, map[string]any{"user": a.User, "csrf_token": a.Session.CSRFToken})
	})
	protected.HandleFunc("GET /api/security/sessions", func(w http.ResponseWriter, r *http.Request) {
		a := r.Context().Value(authContextKey{}).(authContext)
		items, err := auth.ListSessions(r.Context(), a.User.ID)
		if err != nil {
			writeError(w, 500, "load sessions")
			return
		}
		writeJSON(w, 200, map[string]any{"items": items, "current": a.Session.TokenHash})
	})
	protected.HandleFunc("DELETE /api/security/sessions/{hash}", func(w http.ResponseWriter, r *http.Request) {
		a := r.Context().Value(authContextKey{}).(authContext)
		items, _ := auth.ListSessions(r.Context(), a.User.ID)
		found := false
		for _, s := range items {
			if s.TokenHash == r.PathValue("hash") {
				found = true
				break
			}
		}
		if !found {
			writeError(w, 404, "session not found")
			return
		}
		_ = auth.DeleteSession(r.Context(), r.PathValue("hash"))
		w.WriteHeader(204)
	})
	protected.HandleFunc("POST /api/security/password", func(w http.ResponseWriter, r *http.Request) {
		a := r.Context().Value(authContextKey{}).(authContext)
		var in struct{ Current, New string }
		if json.NewDecoder(r.Body).Decode(&in) != nil || len(in.New) < 12 {
			writeError(w, 400, "new password must be at least 12 characters")
			return
		}
		u, _ := auth.UserByID(r.Context(), a.User.ID)
		if !verifyPassword(u.PasswordHash, in.Current) {
			writeError(w, 401, "current password is incorrect")
			return
		}
		_ = auth.UpdatePassword(r.Context(), u.ID, hashPassword(in.New))
		_ = auth.DeleteUserSessions(r.Context(), u.ID, a.Session.TokenHash)
		w.WriteHeader(204)
	})
	protected.HandleFunc("POST /api/security/mfa/setup", func(w http.ResponseWriter, r *http.Request) {
		a := r.Context().Value(authContextKey{}).(authContext)
		if a.User.MFAEnabled {
			writeError(w, http.StatusConflict, "MFA is already enabled")
			return
		}
		u, err := auth.UserByID(r.Context(), a.User.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "load MFA setup")
			return
		}
		secret := decryptSecret(u.MFASecret)
		if secret == "" {
			secret = randomBase32(20)
			if err := auth.UpdateMFA(r.Context(), a.User.ID, encryptSecret(secret), false); err != nil {
				writeError(w, http.StatusInternalServerError, "save MFA setup")
				return
			}
		}
		writeJSON(w, 200, map[string]any{"secret": secret, "otpauth_uri": fmt.Sprintf("otpauth://totp/Hexa.Simulator:%s?secret=%s&issuer=Hexa.Simulator", a.User.Email, secret)})
	})
	protected.HandleFunc("POST /api/security/mfa/enable", func(w http.ResponseWriter, r *http.Request) {
		a := r.Context().Value(authContextKey{}).(authContext)
		var in struct{ Code string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		u, _ := auth.UserByID(r.Context(), a.User.ID)
		if !verifyTOTP(decryptSecret(u.MFASecret), in.Code, time.Now()) {
			writeError(w, 400, "invalid authenticator code")
			return
		}
		_ = auth.UpdateMFA(r.Context(), u.ID, u.MFASecret, true)
		codes := recoveryCodes()
		hashes := make([]string, len(codes))
		for i, c := range codes {
			hashes[i] = hashText(c)
		}
		_ = auth.ReplaceRecoveryCodes(r.Context(), u.ID, hashes)
		writeJSON(w, 200, map[string]any{"recovery_codes": codes})
	})
	protected.HandleFunc("POST /api/security/mfa/disable", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusConflict, "MFA is required for simulator accounts")
	})
	protected.HandleFunc("POST /api/security/recovery-codes", func(w http.ResponseWriter, r *http.Request) {
		a := r.Context().Value(authContextKey{}).(authContext)
		u, _ := auth.UserByID(r.Context(), a.User.ID)
		if !u.MFAEnabled {
			writeError(w, 409, "MFA is not enabled")
			return
		}
		codes := recoveryCodes()
		hashes := make([]string, len(codes))
		for i, c := range codes {
			hashes[i] = hashText(c)
		}
		_ = auth.ReplaceRecoveryCodes(r.Context(), u.ID, hashes)
		writeJSON(w, 200, map[string]any{"recovery_codes": codes})
	})
	protected.HandleFunc("GET /api/admin/users", func(w http.ResponseWriter, r *http.Request) {
		a := r.Context().Value(authContextKey{}).(authContext)
		if a.User.Role != "administrator" {
			writeError(w, 403, "administrator role required")
			return
		}
		items, err := auth.ListUsers(r.Context())
		if err != nil {
			writeError(w, 500, "load users")
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
	})
	protected.HandleFunc("POST /api/admin/users", func(w http.ResponseWriter, r *http.Request) {
		a := r.Context().Value(authContextKey{}).(authContext)
		if a.User.Role != "administrator" {
			writeError(w, 403, "administrator role required")
			return
		}
		var in struct{ Email, DisplayName, Role, Password string }
		if json.NewDecoder(r.Body).Decode(&in) != nil || len(in.Password) < 12 {
			writeError(w, 400, "valid user and 12+ character password required")
			return
		}
		if in.Role != "administrator" && in.Role != "operator" && in.Role != "viewer" {
			writeError(w, 400, "invalid role")
			return
		}
		u, err := auth.CreateUser(r.Context(), strings.ToLower(strings.TrimSpace(in.Email)), strings.TrimSpace(in.DisplayName), in.Role, hashPassword(in.Password))
		if err != nil {
			writeError(w, 400, "create user")
			return
		}
		writeJSON(w, 201, u)
	})
	mux.Handle("/api/", authMiddleware(auth, csrfMiddleware(protected, core)))
	mux.Handle("/", core)
	return securityHeaders(mux)
}

func authMiddleware(store AuthStore, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a, ok := authenticate(r, store)
		if !ok {
			writeError(w, 401, "authentication required")
			return
		}
		_ = store.TouchSession(r.Context(), a.Session.TokenHash)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authContextKey{}, a)))
	})
}
func csrfMiddleware(protected *http.ServeMux, core http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			a := r.Context().Value(authContextKey{}).(authContext)
			if a.User.Role == "viewer" && !strings.HasPrefix(r.URL.Path, "/api/security/") {
				writeError(w, http.StatusForbidden, "viewer role is read-only")
				return
			}
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(a.Session.CSRFToken)) != 1 {
				writeError(w, 403, "invalid CSRF token")
				return
			}
		}
		a := r.Context().Value(authContextKey{}).(authContext)
		if !a.User.MFAEnabled && !strings.HasPrefix(r.URL.Path, "/api/auth/") && !strings.HasPrefix(r.URL.Path, "/api/security/mfa/") {
			writeError(w, http.StatusForbidden, "MFA setup required")
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/auth/") || strings.HasPrefix(r.URL.Path, "/api/security/") || strings.HasPrefix(r.URL.Path, "/api/admin/") {
			protected.ServeHTTP(w, r)
			return
		}
		core.ServeHTTP(w, r)
	})
}
func authenticate(r *http.Request, s AuthStore) (authContext, bool) {
	c, e := r.Cookie("hexa_sim_session")
	if e != nil {
		return authContext{}, false
	}
	ss, e := s.SessionByHash(r.Context(), hashText(c.Value))
	if e != nil || time.Now().After(ss.ExpiresAt) {
		return authContext{}, false
	}
	u, e := s.UserByID(r.Context(), ss.UserID)
	return authContext{u, ss}, e == nil
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}
func setSessionCookie(w http.ResponseWriter, v string) {
	http.SetCookie(w, &http.Cookie{Name: "hexa_sim_session", Value: v, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: false, MaxAge: 43200})
}
func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: "hexa_sim_session", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
}
func remoteIP(r *http.Request) string {
	v := r.RemoteAddr
	if i := strings.LastIndex(v, ":"); i > 0 {
		return strings.Trim(v[:i], "[]")
	}
	return v
}
func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
func randomBase32(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
}
func hashText(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func hashPassword(p string) string {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	dk := pbkdf2([]byte(p), salt, 600000, 32)
	return "pbkdf2-sha256$600000$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(dk)
}
func verifyPassword(encoded, p string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	it, _ := strconv.Atoi(parts[1])
	salt, e1 := base64.RawStdEncoding.DecodeString(parts[2])
	want, e2 := base64.RawStdEncoding.DecodeString(parts[3])
	if e1 != nil || e2 != nil || it < 1 {
		return false
	}
	got := pbkdf2([]byte(p), salt, it, len(want))
	return subtle.ConstantTimeCompare(got, want) == 1
}
func pbkdf2(password, salt []byte, iterations, keyLen int) []byte {
	hLen := 32
	out := make([]byte, 0, keyLen)
	for block := 1; len(out) < keyLen; block++ {
		mac := hmac.New(sha256.New, password)
		mac.Write(salt)
		mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iterations; i++ {
			mac = hmac.New(sha256.New, password)
			mac.Write(u)
			u = mac.Sum(nil)
			for j := 0; j < hLen; j++ {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLen]
}
func verifyTOTP(secret, code string, now time.Time) bool {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false
	}
	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		return false
	}
	for d := -1; d <= 1; d++ {
		counter := uint64(now.Unix()/30 + int64(d))
		b := make([]byte, 8)
		for i := 7; i >= 0; i-- {
			b[i] = byte(counter)
			counter >>= 8
		}
		m := hmac.New(sha1.New, raw)
		m.Write(b)
		h := m.Sum(nil)
		o := h[len(h)-1] & 15
		v := (uint32(h[o])&127)<<24 | uint32(h[o+1])<<16 | uint32(h[o+2])<<8 | uint32(h[o+3])
		if fmt.Sprintf("%06d", v%1000000) == code {
			return true
		}
	}
	return false
}
func recoveryCodes() []string {
	out := make([]string, 8)
	for i := range out {
		out[i] = strings.ToUpper(randomToken(6))
	}
	return out
}

func secretKey() []byte { h := sha256.Sum256([]byte(os.Getenv("SIM_SECURITY_KEY"))); return h[:] }
func encryptSecret(v string) string {
	if v == "" {
		return ""
	}
	block, err := aes.NewCipher(secretKey())
	if err != nil {
		return ""
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return ""
	}
	nonce := make([]byte, g.NonceSize())
	_, _ = rand.Read(nonce)
	return base64.RawStdEncoding.EncodeToString(append(nonce, g.Seal(nil, nonce, []byte(v), nil)...))
}
func decryptSecret(v string) string {
	if v == "" {
		return ""
	}
	raw, err := base64.RawStdEncoding.DecodeString(v)
	if err != nil {
		return ""
	}
	block, err := aes.NewCipher(secretKey())
	if err != nil {
		return ""
	}
	g, err := cipher.NewGCM(block)
	if err != nil || len(raw) < g.NonceSize() {
		return ""
	}
	plain, err := g.Open(nil, raw[:g.NonceSize()], raw[g.NonceSize():], nil)
	if err != nil {
		return ""
	}
	return string(plain)
}

func EnsureBootstrapAdmin(ctx context.Context, store AuthStore, username, password string) error {
	if len(os.Getenv("SIM_SECURITY_KEY")) < 32 {
		return fmt.Errorf("SIM_SECURITY_KEY must be at least 32 characters")
	}
	if strings.TrimSpace(username) == "" {
		username = "hexa-dev"
	}
	return store.EnsureAdmin(ctx, strings.ToLower(strings.TrimSpace(username)), "Simulator Administrator", hashPassword(password))
}
