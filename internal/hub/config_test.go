package hub

import (
	"strings"
	"testing"
)

func TestConfigNeedsAnHTTPSPublicURL(t *testing.T) {
	for _, tc := range []struct {
		url  string
		want string // part of the error, or "" for none
	}{
		{"https://meat-mode.example.com", ""},
		{"https://meat-mode.example.com:8443/", ""},
		{"http://127.0.0.1:8080", ""},
		{"http://localhost:8080", ""},
		{"http://[::1]:8080", ""},
		{"", "not set"},
		{"meat-mode.example.com", "not a URL"},
		{"http://meat-mode.example.com", "https://"},
		{"http://192.0.2.10:8080", "https://"},
		{"ftp://meat-mode.example.com", "https://"},
	} {
		err := Config{PublicURL: tc.url}.Validate()
		if tc.want == "" && err != nil {
			t.Errorf("%q: %v, want no error", tc.url, err)
		}
		if tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("%q: %v, want an error containing %q", tc.url, err, tc.want)
		}
	}
}

func TestConfigPushAllow(t *testing.T) {
	c := Config{PublicURL: "https://meat-mode.example.com", PushAllow: " ntfy.example.com, Push.Example.NET. ,192.0.2.0/24,2001:db8::/48, 198.51.100.7 ,"}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	hosts, cidrs, err := c.PushAllowList()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(hosts, ",") != "ntfy.example.com,push.example.net" {
		t.Errorf("hosts %v", hosts)
	}
	var got []string
	for _, p := range cidrs {
		got = append(got, p.String())
	}
	if strings.Join(got, ",") != "192.0.2.0/24,2001:db8::/48,198.51.100.7/32" {
		t.Errorf("cidrs %v", got)
	}
	for _, bad := range []string{"https://ntfy.example.com", "ntfy.example.com:443", "ntfy.example.com/up", "192.0.2.0/33", "0.0.0.0/0", "198.51.0.0/15", "2001:db8::/47", "::ffff:0.0.0.0/96", "not a host", "-ntfy.example.com"} {
		err := Config{PublicURL: "https://meat-mode.example.com", PushAllow: "ntfy.example.com," + bad}.Validate()
		if err == nil || !strings.Contains(err.Error(), "MEAT_MODE_PUSH_ALLOW") || !strings.Contains(err.Error(), bad) {
			t.Errorf("%q: %v, want an error naming the entry", bad, err)
		}
	}
}
