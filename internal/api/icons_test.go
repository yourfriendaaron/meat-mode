package api

import (
	"strings"
	"testing"
)

func TestSniffIcon(t *testing.T) {
	cases := map[string]struct {
		b    string
		want string
	}{
		"png":            {"\x89PNG\r\n\x1a\n....", IconPNG},
		"ico":            {"\x00\x00\x01\x00\x01\x00rest", IconICO},
		"svg":            {`<svg xmlns="http://www.w3.org/2000/svg"></svg>`, IconSVG},
		"svg with xml":   {"\xef\xbb\xbf<?xml version=\"1.0\"?>\n<!-- made by hand -->\n<!DOCTYPE svg>\n<svg>", IconSVG},
		"html":           {"<html><svg></svg></html>", ""},
		"svgfoo":         {"<svgfoo>", ""},
		"cursor":         {"\x00\x00\x02\x00\x01\x00", ""},
		"empty":          {"", ""},
		"open comment":   {"<!-- <svg>", ""},
		"plain text":     {"hello", ""},
		"too big":        {"\x89PNG\r\n\x1a\n" + strings.Repeat("a", MaxIconBytes), ""},
		"script element": {"<script>alert(1)</script><svg>", ""},
	}
	for name, c := range cases {
		got, ok := SniffIcon([]byte(c.b))
		if got != c.want || ok != (c.want != "") {
			t.Errorf("%s: %q %v, want %q", name, got, ok, c.want)
		}
	}
}

func TestIconHash(t *testing.T) {
	h := IconHash([]byte("<svg></svg>"))
	if !ValidIconHash(h) || h == IconHash([]byte("<svg> </svg>")) {
		t.Errorf("hash %q", h)
	}
	for _, s := range []string{"", strings.Repeat("A", 32), strings.Repeat("a", 31), "../" + strings.Repeat("a", 29)} {
		if ValidIconHash(s) {
			t.Errorf("ValidIconHash(%q)", s)
		}
	}
}
