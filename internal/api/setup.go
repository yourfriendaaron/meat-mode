package api

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
	// TailscaleOnly is set when the user chose no password, keeping the
	// hub on a private network.
	TailscaleOnly bool `json:"tailscaleOnly"`
}

// SetupRecord is POST /api/setup.
type SetupRecord struct {
	Status string `json:"status"` // "done" or "dismissed"
}
