// Package auth implements local user authentication: a SQLite user store
// (bcrypt password hashes) and stateless JWT bearer tokens (HS256).
package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	_ "modernc.org/sqlite"
)

// Errors surfaced to the API layer.
var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidToken       = errors.New("invalid token")
)

// TokenTTL is the bearer token lifetime.
const TokenTTL = 24 * time.Hour

// User is the public user model (no secrets).
type User struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
}

// Service is the auth facade used by the HTTP layer.
type Service struct {
	db     *sql.DB
	secret []byte
}

// Open opens (creating if needed) the SQLite database at path and ensures
// the users schema exists. The JWT secret is used to sign tokens; when it is
// empty a random per-process secret is generated (sessions do not survive
// restarts in that case).
func Open(path, secret string, log *slog.Logger) (*Service, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create data directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // sqlite: single writer avoids SQLITE_BUSY
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'admin',
		created_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create users table: %w", err)
	}
	if secret == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			db.Close()
			return nil, err
		}
		secret = hex.EncodeToString(b)
		log.Warn("ULTRAV_JWT_SECRET not set: generated an ephemeral secret (sessions will not survive restarts)")
	}
	return &Service{db: db, secret: []byte(secret)}, nil
}

// Close releases the database handle.
func (s *Service) Close() error { return s.db.Close() }

// BootstrapAdmin creates the initial admin user when the database is empty.
// The password is taken from cfg, or randomly generated and logged once when
// empty (never a silent default credential).
func (s *Service) BootstrapAdmin(username, password string, log *slog.Logger) error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if username == "" {
		username = "admin"
	}
	generated := false
	if password == "" {
		b := make([]byte, 9)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		password = hex.EncodeToString(b)
		generated = true
	}
	if _, err := s.CreateUser(username, password, "admin"); err != nil {
		return err
	}
	if generated {
		log.Warn("no users found: created initial admin with a generated password",
			"username", username, "password", password,
			"hint", "set ULTRAV_ADMIN_PASSWORD or see this log line once; change it later")
	} else {
		log.Info("no users found: created initial admin user", "username", username)
	}
	return nil
}

// CreateUser inserts a user with a bcrypt-hashed password.
func (s *Service) CreateUser(username, password, role string) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	res, err := s.db.Exec(`INSERT INTO users (username, password_hash, role) VALUES (?, ?, ?)`,
		username, string(hash), role)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.byID(id)
}

// Login validates credentials and returns a signed token plus the user.
func (s *Service) Login(username, password string) (token string, user *User, err error) {
	var (
		id             int64
		hash           string
		u              User
		createdAtUnix int64
	)
	err = s.db.QueryRow(`SELECT id, username, password_hash, role, created_at FROM users WHERE username = ?`, username).
		Scan(&id, &u.Username, &hash, &u.Role, &createdAtUnix)
	u.CreatedAt = time.Unix(createdAtUnix, 0).UTC()
	if errors.Is(err, sql.ErrNoRows) {
		// burn comparable time to avoid user enumeration via timing
		_ = bcrypt.CompareHashAndPassword(
			[]byte("$2a$10$7EqJtq98hPqEX7fNZaFWoOhi5B0X8fSK0MO9zpxHYK5cbFOqjS0P2"), []byte(password))
		return "", nil, ErrInvalidCredentials
	}
	if err != nil {
		return "", nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return "", nil, ErrInvalidCredentials
	}
	u.ID = id
	now := time.Now()
	claims := jwt.MapClaims{
		"sub":   u.Username,
		"uid":   u.ID,
		"role":  u.Role,
		"iat":   now.Unix(),
		"exp":   now.Add(TokenTTL).Unix(),
	}
	token, err = jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	if err != nil {
		return "", nil, err
	}
	return token, &u, nil
}

// Verify validates a bearer token and returns the user it identifies.
func (s *Service) Verify(tokenStr string) (*User, error) {
	tok, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return s.secret, nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !tok.Valid {
		return nil, ErrInvalidToken
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return nil, ErrInvalidToken
	}
	username, _ := claims["sub"].(string)
	if username == "" {
		return nil, ErrInvalidToken
	}
	u, err := s.byUsername(username)
	if err != nil {
		return nil, ErrInvalidToken
	}
	return u, nil
}

func (s *Service) byID(id int64) (*User, error) {
	u := new(User)
	var ts int64
	err := s.db.QueryRow(`SELECT id, username, role, created_at FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Username, &u.Role, &ts)
	u.CreatedAt = time.Unix(ts, 0).UTC()
	return u, err
}

func (s *Service) byUsername(username string) (*User, error) {
	u := new(User)
	var ts int64
	err := s.db.QueryRow(`SELECT id, username, role, created_at FROM users WHERE username = ?`, username).
		Scan(&u.ID, &u.Username, &u.Role, &ts)
	u.CreatedAt = time.Unix(ts, 0).UTC()
	return u, err
}
