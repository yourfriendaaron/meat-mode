// Package auth hashes and checks the app's login password. A hash is an
// Argon2id string in PHC format, the form `meat-mode hash-password` prints
// and MEAT_MODE_PASSWORD_HASH holds:
//
//	$argon2id$v=19$m=65536,t=3,p=1$<salt>$<key>
//
// with salt and key in unpadded standard base64.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Parameters for new hashes, after OWASP's Argon2id guidance.
const (
	defaultMemory  = 64 * 1024 // KiB
	defaultTime    = 3
	defaultThreads = 1
	saltLen        = 16
	keyLen         = 32
)

// Upper bounds on a parsed hash's parameters, so a hash someone else wrote
// cannot make each login take gigabytes or minutes.
const (
	maxMemory  = 1024 * 1024 // KiB, 1 GiB
	maxTime    = 20
	maxThreads = 16
	maxSalt    = 64
	maxKey     = 64
)

// ErrFormat is returned for a string that is not an Argon2id PHC hash within
// the bounds above.
var ErrFormat = errors.New("not an Argon2id hash such as $argon2id$v=19$m=65536,t=3,p=1$<salt>$<hash>; make one with `meat-mode hash-password`")

// Hash is a parsed password hash.
type Hash struct {
	memory  uint32
	time    uint32
	threads uint8
	salt    []byte
	key     []byte
}

// HashPassword hashes password with a fresh salt and returns the PHC string.
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	h := Hash{memory: defaultMemory, time: defaultTime, threads: defaultThreads, salt: salt}
	h.key = h.derive(password, keyLen)
	return h.String(), nil
}

// ParseHash parses a PHC string.
func ParseHash(s string) (*Hash, error) {
	parts := strings.Split(s, "$")
	// "", "argon2id", "v=19", "m=..,t=..,p=..", salt, key
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return nil, ErrFormat
	}
	h := &Hash{}
	seen := map[string]bool{}
	for kv := range strings.SplitSeq(parts[3], ",") {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || seen[k] {
			return nil, ErrFormat
		}
		seen[k] = true
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return nil, ErrFormat
		}
		switch k {
		case "m":
			h.memory = uint32(n)
		case "t":
			h.time = uint32(n)
		case "p":
			if n > maxThreads {
				return nil, ErrFormat
			}
			h.threads = uint8(n)
		default:
			return nil, ErrFormat
		}
	}
	if len(seen) != 3 || h.time < 1 || h.time > maxTime || h.threads < 1 ||
		h.memory < 8*uint32(h.threads) || h.memory > maxMemory {
		return nil, ErrFormat
	}
	var err error
	if h.salt, err = base64.RawStdEncoding.DecodeString(parts[4]); err != nil || len(h.salt) < 8 || len(h.salt) > maxSalt {
		return nil, ErrFormat
	}
	if h.key, err = base64.RawStdEncoding.DecodeString(parts[5]); err != nil || len(h.key) < 16 || len(h.key) > maxKey {
		return nil, ErrFormat
	}
	return h, nil
}

// Verify reports whether password matches the hash.
func (h *Hash) Verify(password string) bool {
	return subtle.ConstantTimeCompare(h.derive(password, uint32(len(h.key))), h.key) == 1
}

// String is the PHC form.
func (h *Hash) String() string {
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", h.memory, h.time, h.threads,
		base64.RawStdEncoding.EncodeToString(h.salt), base64.RawStdEncoding.EncodeToString(h.key))
}

func (h *Hash) derive(password string, n uint32) []byte {
	return argon2.IDKey([]byte(password), h.salt, h.time, h.memory, h.threads, n)
}
