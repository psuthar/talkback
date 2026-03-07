package invitations

import (
	"testing"
)

func TestHashToken(t *testing.T) {
	// Hash is deterministic
	h1 := HashToken("abc")
	h2 := HashToken("abc")
	if h1 != h2 {
		t.Fatalf("HashToken not deterministic: %q != %q", h1, h2)
	}
	// Different input => different hash
	h3 := HashToken("abd")
	if h1 == h3 {
		t.Fatalf("HashToken should differ for different inputs")
	}
	// Output is 64-char hex (SHA-256)
	if len(h1) != 64 {
		t.Errorf("expected 64-char hex, got len %d", len(h1))
	}
	for _, c := range h1 {
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') {
			continue
		}
		t.Errorf("HashToken output should be hex, got %q", h1)
		break
	}
}

func TestTokenMatches(t *testing.T) {
	raw := "test-token-123"
	hash := HashToken(raw)
	if !TokenMatches(raw, hash) {
		t.Error("TokenMatches(raw, HashToken(raw)) should be true")
	}
	if TokenMatches("wrong", hash) {
		t.Error("TokenMatches(wrong, hash) should be false")
	}
	if TokenMatches(raw, "wronghash") {
		t.Error("TokenMatches(raw, wronghash) should be false")
	}
	// Empty
	if TokenMatches("", HashToken("")) {
		// HashToken("") is valid; matching empty to its hash is a edge case
	}
	if TokenMatches("x", HashToken("")) {
		t.Error("TokenMatches(x, hash of empty) should be false")
	}
}

func TestGenerateToken(t *testing.T) {
	raw, hash, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if raw == "" || hash == "" {
		t.Fatal("GenerateToken should return non-empty raw and hash")
	}
	if !TokenMatches(raw, hash) {
		t.Error("GenerateToken: raw should match hash")
	}
	// Second call should produce different token
	raw2, hash2, _ := GenerateToken()
	if raw == raw2 || hash == hash2 {
		t.Error("GenerateToken should produce different tokens each time")
	}
}
