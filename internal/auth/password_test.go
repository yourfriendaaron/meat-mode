package auth

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestHashRoundTrip(t *testing.T) {
	s, err := HashPassword("water the plants")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s, "$argon2id$v=19$m=65536,t=3,p=1$") {
		t.Fatalf("hash %q does not use the default parameters", s)
	}
	h, err := ParseHash(s)
	if err != nil {
		t.Fatal(err)
	}
	if h.String() != s {
		t.Errorf("String() = %q, want %q", h.String(), s)
	}
	if !h.Verify("water the plants") {
		t.Error("the right password does not verify")
	}
	if h.Verify("water the plant") || h.Verify("") {
		t.Error("a wrong password verifies")
	}
	other, _ := HashPassword("water the plants")
	if other == s {
		t.Error("two hashes of one password share a salt")
	}
}

func TestParseHashRefuses(t *testing.T) {
	b64 := base64.RawStdEncoding.EncodeToString
	salt, key := b64([]byte("somesaltsomesalt")), b64([]byte("keykeykeykeykeykeykeykeykeykeyke"))
	ok := "$argon2id$v=19$m=65536,t=3,p=1$" + salt + "$" + key
	if _, err := ParseHash(ok); err != nil {
		t.Fatalf("valid hash refused: %v", err)
	}
	for _, s := range []string{
		"",
		"hunter2",
		"$argon2i$v=19$m=65536,t=3,p=1$" + salt + "$" + key,
		"$argon2id$v=16$m=65536,t=3,p=1$" + salt + "$" + key,
		"$argon2id$v=19$m=65536,t=3$" + salt + "$" + key,
		"$argon2id$v=19$m=65536,t=3,p=1,x=2$" + salt + "$" + key,
		"$argon2id$v=19$m=65536,m=1,p=1$" + salt + "$" + key,
		"$argon2id$v=19$m=65536,t=0,p=1$" + salt + "$" + key,
		"$argon2id$v=19$m=65536,t=3,p=0$" + salt + "$" + key,
		"$argon2id$v=19$m=4194304,t=3,p=1$" + salt + "$" + key, // 4 GiB
		"$argon2id$v=19$m=65536,t=1000,p=1$" + salt + "$" + key,
		"$argon2id$v=19$m=65536,t=3,p=255$" + salt + "$" + key,
		"$argon2id$v=19$m=65536,t=3,p=1$!!$" + key,
		"$argon2id$v=19$m=65536,t=3,p=1$" + salt + "$" + "c2hvcnQ",
		"$argon2id$v=19$m=65536,t=3,p=1$" + salt + "$" + key + "$extra",
		"'$argon2id$v=19$m=65536,t=3,p=1$" + salt + "$" + key + "'",
	} {
		if _, err := ParseHash(s); err == nil {
			t.Errorf("ParseHash(%q) accepted", s)
		}
	}
}
