// Package hub serves the app API, the bridge API and the embedded frontend.
// docs/api.md describes the routes.
package hub

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/coder/safedial"

	"github.com/yourfriendaaron/meat-mode/internal/auth"
	"github.com/yourfriendaaron/meat-mode/internal/netguard"
)

// Config is the hub's settings, read from MEAT_MODE_* environment variables.
type Config struct {
	Listen    string // MEAT_MODE_LISTEN, default :8080
	DataDir   string // MEAT_MODE_DATA_DIR
	PublicURL string // MEAT_MODE_PUBLIC_URL
	User      string // MEAT_MODE_USER, default "user"; notes are authored <User>-app
	// AllowedHosts is MEAT_MODE_ALLOWED_HOSTS: comma-separated host names
	// the app answers to besides localhost and the PublicURL host.
	AllowedHosts string
	// PasswordHash is MEAT_MODE_PASSWORD_HASH: the Argon2id PHC string of
	// the login password. Login is on when it is set.
	PasswordHash string
	// PasswordHashFile is MEAT_MODE_PASSWORD_HASH_FILE, a file holding the
	// hash instead. LoadPasswordHash reads it into PasswordHash.
	PasswordHashFile string
	// NtfyURL is MEAT_MODE_NTFY_URL. The hub only reports whether it is
	// set, in the connection check; internal/notify does the sending.
	NtfyURL string
	// PushAllow is MEAT_MODE_PUSH_ALLOW: comma-separated host names and
	// CIDRs of push servers the Android app may register that are not at
	// global addresses (docs/api.md, Devices). PushAllowList parses it.
	PushAllow string
	// TrustedNetworks is MEAT_MODE_TRUSTED_NETWORKS: the operator's own
	// networks outside the private set, as netguard.ParseTrusted reads them.
	TrustedNetworks string
}

// ConfigFromEnv reads Config from the environment.
func ConfigFromEnv() Config {
	return Config{
		Listen:    envOr("MEAT_MODE_LISTEN", ":8080"),
		DataDir:   os.Getenv("MEAT_MODE_DATA_DIR"),
		PublicURL: os.Getenv("MEAT_MODE_PUBLIC_URL"),
		User:      envOr("MEAT_MODE_USER", "user"),

		AllowedHosts:     os.Getenv("MEAT_MODE_ALLOWED_HOSTS"),
		PasswordHash:     strings.TrimSpace(os.Getenv("MEAT_MODE_PASSWORD_HASH")),
		PasswordHashFile: os.Getenv("MEAT_MODE_PASSWORD_HASH_FILE"),
		NtfyURL:          strings.TrimSpace(os.Getenv("MEAT_MODE_NTFY_URL")),
		PushAllow:        os.Getenv("MEAT_MODE_PUSH_ALLOW"),
		TrustedNetworks:  os.Getenv(netguard.EnvTrusted),
	}
}

// LoadPasswordHash reads PasswordHashFile into PasswordHash. Setting both is
// an error, since it is unclear which one the operator meant.
func (c *Config) LoadPasswordHash() error {
	if c.PasswordHashFile == "" {
		return nil
	}
	if c.PasswordHash != "" {
		return errors.New("set MEAT_MODE_PASSWORD_HASH or MEAT_MODE_PASSWORD_HASH_FILE, not both")
	}
	b, err := os.ReadFile(c.PasswordHashFile)
	if err != nil {
		return fmt.Errorf("MEAT_MODE_PASSWORD_HASH_FILE: %w", err)
	}
	h := strings.TrimSpace(string(b))
	if h == "" {
		return fmt.Errorf("MEAT_MODE_PASSWORD_HASH_FILE %s is empty", c.PasswordHashFile)
	}
	c.PasswordHash, c.PasswordHashFile = h, ""
	return nil
}

// Validate refuses a hub without an HTTPS public address, with a password
// hash it cannot read, or with a list in MEAT_MODE_PUSH_ALLOW or
// MEAT_MODE_TRUSTED_NETWORKS it cannot parse. Browsers allow Web Push and
// the installed app only in a secure context, and without a login the app
// is meant to be opened over a private network's HTTPS address
// (docs/deploy.md). Plain HTTP is accepted on loopback only, for
// development and tests.
func (c Config) Validate() error {
	if c.PasswordHash != "" && c.PasswordHashFile != "" {
		return errors.New("set MEAT_MODE_PASSWORD_HASH or MEAT_MODE_PASSWORD_HASH_FILE, not both")
	}
	if c.PasswordHash != "" {
		if _, err := auth.ParseHash(c.PasswordHash); err != nil {
			return fmt.Errorf("the login password hash is %w", err)
		}
	}
	if _, _, err := c.PushAllowList(); err != nil {
		return err
	}
	if _, err := netguard.ParseTrusted(c.TrustedNetworks); err != nil {
		return err
	}
	if c.PublicURL == "" {
		return errors.New("MEAT_MODE_PUBLIC_URL is not set: set it to the HTTPS address you open the app at, such as https://meat-mode.example.com")
	}
	u, err := url.Parse(c.PublicURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("MEAT_MODE_PUBLIC_URL %q is not a URL such as https://meat-mode.example.com", c.PublicURL)
	}
	switch {
	case u.Scheme == "https":
		return nil
	case u.Scheme == "http" && isLoopback(u.Hostname()):
		return nil
	}
	return fmt.Errorf("MEAT_MODE_PUBLIC_URL %q must start with https:// (plain http:// is accepted for localhost only)", c.PublicURL)
}

// PushAllowList parses PushAllow into host names, lower case, and CIDRs. A
// bare address counts as a CIDR of that one address. An entry that is
// neither a host name nor a CIDR is an error naming it.
func (c Config) PushAllowList() (hosts []string, cidrs []netip.Prefix, err error) {
	for e := range strings.SplitSeq(c.PushAllow, ",") {
		e = strings.TrimSpace(e)
		switch {
		case e == "":
			continue
		case strings.Contains(e, "/"):
			p, err := safedial.ParseAllowedPrefix(e)
			if err != nil {
				return nil, nil, fmt.Errorf("MEAT_MODE_PUSH_ALLOW entry %q is not a CIDR such as 192.0.2.0/24 or 2001:db8::/32", e)
			}
			cidrs = append(cidrs, p.Masked())
		default:
			if a, err := netip.ParseAddr(e); err == nil {
				a = a.Unmap()
				cidrs = append(cidrs, netip.PrefixFrom(a, a.BitLen()))
				continue
			}
			h := strings.ToLower(strings.TrimSuffix(e, "."))
			if !hostNameRE.MatchString(h) || len(h) > 253 {
				return nil, nil, fmt.Errorf("MEAT_MODE_PUSH_ALLOW entry %q is not a host name such as ntfy.example.com or a CIDR such as 192.0.2.0/24; give a host name without a scheme, port or path", e)
			}
			hosts = append(hosts, h)
		}
	}
	return hosts, cidrs, nil
}

// hostNameRE matches a DNS host name: dot-separated labels of letters,
// digits and inner hyphens, at most 63 characters each.
var hostNameRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
