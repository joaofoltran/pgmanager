package secret

import (
	"testing"
)

func TestRoundTrip(t *testing.T) {
	box, err := NewBox("test-passphrase-123")
	if err != nil {
		t.Fatal(err)
	}

	tests := []string{
		"simple-password",
		"p@$$w0rd!#%^&*()",
		"unicode: 日本語パスワード",
		"a",
		"very-long-password-" + string(make([]byte, 1000)),
	}

	for _, plain := range tests {
		enc, err := box.Encrypt(plain)
		if err != nil {
			t.Fatalf("encrypt %q: %v", plain, err)
		}
		if !IsEncrypted(enc) {
			t.Fatalf("expected encrypted prefix, got %q", enc)
		}
		dec, err := box.Decrypt(enc)
		if err != nil {
			t.Fatalf("decrypt: %v", err)
		}
		if dec != plain {
			t.Fatalf("got %q, want %q", dec, plain)
		}
	}
}

func TestEmptyPassthrough(t *testing.T) {
	box, err := NewBox("key")
	if err != nil {
		t.Fatal(err)
	}

	enc, err := box.Encrypt("")
	if err != nil {
		t.Fatal(err)
	}
	if enc != "" {
		t.Fatalf("expected empty, got %q", enc)
	}

	dec, err := box.Decrypt("")
	if err != nil {
		t.Fatal(err)
	}
	if dec != "" {
		t.Fatalf("expected empty, got %q", dec)
	}
}

func TestPlaintextPassthrough(t *testing.T) {
	box, err := NewBox("key")
	if err != nil {
		t.Fatal(err)
	}

	dec, err := box.Decrypt("legacy-plain-password")
	if err != nil {
		t.Fatal(err)
	}
	if dec != "legacy-plain-password" {
		t.Fatalf("got %q, want %q", dec, "legacy-plain-password")
	}
}

func TestWrongKey(t *testing.T) {
	box1, _ := NewBox("key-one")
	box2, _ := NewBox("key-two")

	enc, err := box1.Encrypt("secret")
	if err != nil {
		t.Fatal(err)
	}

	_, err = box2.Decrypt(enc)
	if err == nil {
		t.Fatal("expected decryption error with wrong key")
	}
}

func TestUniqueNonces(t *testing.T) {
	box, _ := NewBox("key")
	enc1, _ := box.Encrypt("same-input")
	enc2, _ := box.Encrypt("same-input")
	if enc1 == enc2 {
		t.Fatal("two encryptions of same plaintext should produce different ciphertext")
	}
}

func TestEmptyPassphrase(t *testing.T) {
	_, err := NewBox("")
	if err == nil {
		t.Fatal("expected error for empty passphrase")
	}
}

func TestIsEncrypted(t *testing.T) {
	if IsEncrypted("plain") {
		t.Fatal("plain text should not be detected as encrypted")
	}
	if !IsEncrypted("enc:v1:AAAA") {
		t.Fatal("prefixed value should be detected as encrypted")
	}
	if IsEncrypted("") {
		t.Fatal("empty should not be encrypted")
	}
}
