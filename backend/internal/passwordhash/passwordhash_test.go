package passwordhash

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// @spec ACCT-010
func TestHash_RoundTripsAndEmbedsParameters(t *testing.T) {
	const pw = "correct horse battery staple"

	encoded, err := Hash(pw)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("encoded hash missing argon2id header with embedded params: %q", encoded)
	}

	ok, err := Verify(encoded, pw)
	if err != nil {
		t.Fatalf("Verify(correct): %v", err)
	}
	if !ok {
		t.Fatal("Verify(correct) = false, want true")
	}
}

// @spec ACCT-010
func TestHash_DistinctSaltPerCall(t *testing.T) {
	a, err := Hash("same-password")
	if err != nil {
		t.Fatalf("Hash a: %v", err)
	}
	b, err := Hash("same-password")
	if err != nil {
		t.Fatalf("Hash b: %v", err)
	}
	if a == b {
		t.Fatal("two hashes of the same password are identical; salt is not random")
	}
}

// @spec ACCT-011
func TestVerify_WrongPasswordIsFalseNotError(t *testing.T) {
	encoded, err := Hash("the-real-password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	ok, err := Verify(encoded, "not-the-password")
	if err != nil {
		t.Fatalf("Verify(wrong) returned error, want nil: %v", err)
	}
	if ok {
		t.Fatal("Verify(wrong) = true, want false")
	}
}

// @spec ACCT-011
func TestVerify_MalformedHashIsError(t *testing.T) {
	for _, enc := range []string{
		"",
		"plaintext",
		"$argon2id$v=19$m=19456,t=2$onlyfourfields",
		"$argon2id$v=1$m=19456,t=2,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=x,t=2,p=1$c2FsdA$aGFzaA",
	} {
		if _, err := Verify(enc, "whatever"); err == nil {
			t.Fatalf("Verify(%q) returned nil error, want ErrMalformedHash", enc)
		}
	}
}

// @spec ACCT-011, ACCT-018
func TestVerify_AcceptsBcryptHash(t *testing.T) {
	const pw = "legacy-bcrypt-account"
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt.GenerateFromPassword: %v", err)
	}

	ok, err := Verify(string(b), pw)
	if err != nil {
		t.Fatalf("Verify(bcrypt, correct): %v", err)
	}
	if !ok {
		t.Fatal("Verify(bcrypt, correct) = false, want true")
	}

	ok, err = Verify(string(b), "wrong")
	if err != nil {
		t.Fatalf("Verify(bcrypt, wrong) error: %v", err)
	}
	if ok {
		t.Fatal("Verify(bcrypt, wrong) = true, want false")
	}
}
