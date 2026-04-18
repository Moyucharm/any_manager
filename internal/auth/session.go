package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Session struct {
	AuthVersion int   `json:"auth_version"`
	ExpiresAt   int64 `json:"exp"`
}

type SessionManager struct {
	key        []byte
	cookieName string
	secure     bool
	ttl        time.Duration
}

func NewSessionManager(masterKey, cookieName string, secure bool, ttl time.Duration) *SessionManager {
	hash := sha256.Sum256([]byte("session:" + masterKey))
	return &SessionManager{
		key:        hash[:],
		cookieName: cookieName,
		secure:     secure,
		ttl:        ttl,
	}
}

func (m *SessionManager) CookieName() string {
	return m.cookieName
}

func (m *SessionManager) TTL() time.Duration {
	return m.ttl
}

func (m *SessionManager) Secure() bool {
	return m.secure
}

func (m *SessionManager) Encode(authVersion int, now time.Time) (string, error) {
	session := Session{
		AuthVersion: authVersion,
		ExpiresAt:   now.Add(m.ttl).Unix(),
	}
	payload, err := json.Marshal(session)
	if err != nil {
		return "", fmt.Errorf("marshal session: %w", err)
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	signature := m.sign(encodedPayload)
	return encodedPayload + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (m *SessionManager) Decode(value string, now time.Time) (Session, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return Session{}, fmt.Errorf("invalid session format")
	}
	if !hmac.Equal(m.sign(parts[0]), mustDecodeBase64(parts[1])) {
		return Session{}, fmt.Errorf("invalid session signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Session{}, fmt.Errorf("decode session payload: %w", err)
	}
	var session Session
	if err := json.Unmarshal(payload, &session); err != nil {
		return Session{}, fmt.Errorf("unmarshal session payload: %w", err)
	}
	if now.Unix() >= session.ExpiresAt {
		return Session{}, fmt.Errorf("session expired")
	}
	return session, nil
}

func (m *SessionManager) sign(payload string) []byte {
	mac := hmac.New(sha256.New, m.key)
	_, _ = mac.Write([]byte(payload))
	return mac.Sum(nil)
}

func mustDecodeBase64(value string) []byte {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil
	}
	return decoded
}
