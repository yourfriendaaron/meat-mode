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
