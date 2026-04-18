package auth

import (
	"testing"
	"time"
)

func TestSessionManagerEncodeDecode(t *testing.T) {
	t.Parallel()
	manager := NewSessionManager("test-master-key", "test_cookie", false, time.Hour)
	now := time.Unix(1_700_000_000, 0).UTC()
	value, err := manager.Encode(3, now)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	session, err := manager.Decode(value, now.Add(30*time.Minute))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if session.AuthVersion != 3 {
		t.Fatalf("AuthVersion = %d, want 3", session.AuthVersion)
	}
	if session.ExpiresAt <= now.Unix() {
		t.Fatalf("ExpiresAt = %d, want future", session.ExpiresAt)
	}
	if _, err := manager.Decode(value+"tampered", now); err == nil {
		t.Fatalf("Decode() succeeded for tampered session")
	}
	if _, err := manager.Decode(value, now.Add(2*time.Hour)); err == nil {
		t.Fatalf("Decode() succeeded for expired session")
	}
}
