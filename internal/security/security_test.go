package security

import "testing"

func TestCipherEncryptDecrypt(t *testing.T) {
	t.Parallel()
	cipher, err := NewCipher("test-master-key")
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}
	secret := "sk-test-secret"
	encrypted, err := cipher.Encrypt(secret)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if encrypted == secret {
		t.Fatalf("Encrypt() returned plaintext")
	}
	decrypted, err := cipher.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if decrypted != secret {
		t.Fatalf("Decrypt() = %q, want %q", decrypted, secret)
	}
}

func TestHasherHashVerify(t *testing.T) {
	t.Parallel()
	hasher := NewHasher()
	hash, err := hasher.Hash("downstream-secret")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	ok, err := hasher.Verify("downstream-secret", hash)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !ok {
		t.Fatalf("Verify() = false, want true")
	}
	ok, err = hasher.Verify("wrong-secret", hash)
	if err != nil {
		t.Fatalf("Verify() with wrong secret error = %v", err)
	}
	if ok {
		t.Fatalf("Verify() with wrong secret = true, want false")
	}
}
