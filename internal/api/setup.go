package api

import "time"

// Setup is GET /api/setup: what the app needs to decide whether to offer
// the setup wizard, and what the wizard shows as already set.
type Setup struct {
	// Status is "new" (no items, no machines, no record: offer the hub
	// setup), "in-use" (items or machines but no record), "done" or
	// "dismissed".
	Status string `json:"status"`
	// User is the name kept in the app, or MEAT_MODE_USER.
	User string `json:"user"`
	// TimeZone is the IANA time zone kept in the app, "" until chosen.
	TimeZone string `json:"timeZone"`
	// Password is "env" when MEAT_MODE_PASSWORD_HASH or _FILE sets it,
	// "app" when it was set in the app, "none" otherwise.
	Password string `json:"password"`
}

// SetupRecord is POST /api/setup.
type SetupRecord struct {
	Status string `json:"status"` // "done" or "dismissed"
}

// SetupClaim is POST /api/setup/claim: the setup code from the hub's log
// or its setup-code file, and the password to set.
type SetupClaim struct {
	Code     string `json:"code"`
	Password string `json:"password"`
}

// PasswordChange is PUT /api/password.
type PasswordChange struct {
	Current  string `json:"current"`
	Password string `json:"password"`
}

// SetupStatus is GET /api/setup/status: what the connection check cannot
// learn from the browser alone.
type SetupStatus struct {
	// Login is "environment" when MEAT_MODE_PASSWORD_HASH or _FILE sets
	// the password, "app" when it was set in the app, "none" otherwise.
	Login string `json:"login"`
	// HostMatches is whether this request came in under the host of
	// MEAT_MODE_PUBLIC_URL.
	HostMatches bool `json:"hostMatches"`
	// PublicURL is MEAT_MODE_PUBLIC_URL, the address the hub tells people
	// to open.
	PublicURL string `json:"publicUrl"`
	// PushKeys is whether the hub has Web Push keys to hand out.
	PushKeys bool `json:"pushKeys"`
	// Ntfy is whether MEAT_MODE_NTFY_URL is set.
	Ntfy bool `json:"ntfy"`
	// Time is the hub's clock.
	Time time.Time `json:"time"`
	// PublicDNS is the latest public DNS check of MEAT_MODE_PUBLIC_URL.
	PublicDNS PublicDNS `json:"publicDns"`
	// TailscaleIdentity is whether this request carried a non-empty
	// Tailscale-User-Login header, as Tailscale Serve sets for a tailnet
	// user. It reports presence only, never the value.
	TailscaleIdentity bool `json:"tailscaleIdentity"`
}

// Public DNS check states (docs/api.md, Private network only).
const (
	DNSPrivate   = "private"   // no public record, or only private addresses
	DNSPublic    = "public"    // a public resolver returned a global address
	DNSUnchecked = "unchecked" // no public resolver answered, or no check ran yet
	DNSSkipped   = "skipped"   // the host is an IP address or localhost
)

// PublicDNS is what a public resolver says MEAT_MODE_PUBLIC_URL's host
// resolves to.
type PublicDNS struct {
	State string `json:"state"`
	// Addresses are the global addresses found, for DNSPublic only.
	Addresses []string `json:"addresses,omitempty"`
	// CheckedAt is when the check last got an answer or gave up, null
	// before the first.
	CheckedAt *time.Time `json:"checkedAt"`
}
