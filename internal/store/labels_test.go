package store

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestCleanLabels(t *testing.T) {
	got, err := cleanLabels([]string{" garden ", "", "Errands", "errands", "HUMAN", "area:home", "läden"})
	must(t, err)
	if want := []string{"garden", "Errands", "area:home", "läden"}; !slices.Equal(got, want) {
		t.Fatalf("cleanLabels = %q, want %q", got, want)
	}
	if got, _ := cleanLabels(nil); got == nil || len(got) != 0 {
		t.Fatalf("cleanLabels(nil) = %#v, want an empty list", got)
	}
	many := make([]string, 21)
	for i := range many {
		many[i] = strings.Repeat("a", i+1)
	}
	for _, bad := range [][]string{{"two words"}, {"a,b"}, {"tab\there"}, {strings.Repeat("x", 51)}, many} {
		if _, err := cleanLabels(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("cleanLabels(%q) = %v, want ErrInvalid", bad, err)
		}
	}
	if _, err := cleanLabels([]string{strings.Repeat("ö", 50)}); err != nil {
		t.Errorf("a 50-character tag: %v", err)
	}
}
