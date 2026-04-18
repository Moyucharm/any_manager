package auth

import (
	"net/http"
	"time"
)

func (m *SessionManager) SetCookie(w http.ResponseWriter, authVersion int, now time.Time) error {
	value, err := m.Encode(authVersion, now)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     m.cookieName,
		Value:    value,
		Path:     "/admin",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  now.Add(m.ttl),
		MaxAge:   int(m.ttl.Seconds()),
	})
	return nil
}

func (m *SessionManager) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     m.cookieName,
		Value:    "",
		Path:     "/admin",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
}
