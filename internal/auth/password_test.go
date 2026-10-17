package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	password := "A local test passphrase only"
	first, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("each password hash must use an independent random salt")
	}
	if !strings.HasPrefix(first, "$argon2id$v=19$m=65536,t=3,p=1$") {
		t.Fatalf("unexpected parameter encoding: %s", first)
	}
	if ok, err := VerifyPassword(first, password); err != nil || !ok {
		t.Fatalf("correct password rejected: ok=%v err=%v", ok, err)
	}
	if ok, err := VerifyPassword(first, "incorrect password"); err != nil || ok {
		t.Fatalf("incorrect password accepted or verification failed: ok=%v err=%v", ok, err)
	}
	if strings.Contains(first, password) {
		t.Fatal("plaintext password appears in encoded hash")
	}
}

func TestPasswordBoundsAndMalformedHash(t *testing.T) {
	for _, password := range []string{"short", strings.Repeat("x", 1025), string([]byte{0xff})} {
		if _, err := HashPassword(password); err == nil {
			t.Fatal("invalid password accepted")
		}
	}
	for _, encoded := range []string{
		"", "$argon2id$v=19$m=4294967295,t=3,p=1$abcdefghijklmnopqrstuv$abcdefghijklmnopqrstuv",
		"$argon2id$v=19$m=65536,t=0,p=1$abcdefghijklmnopqrstuv$abcdefghijklmnopqrstuv",
		"$argon2id$v=19$m=65536,t=3,p=0$abcdefghijklmnopqrstuv$abcdefghijklmnopqrstuv",
		"$argon2id$v=19$m=65536,t=3,p=1$bad$bad", "$argon2i$v=19$m=65536,t=3,p=1$bad$bad",
	} {
		if ok, err := VerifyPassword(encoded, "test passphrase"); ok || !errors.Is(err, ErrInvalidHash) {
			t.Fatalf("malformed hash accepted: ok=%v err=%v", ok, err)
		}
	}
}
