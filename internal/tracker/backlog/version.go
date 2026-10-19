package backlog

import (
	"fmt"
	"strconv"
	"strings"
)

// checkVersion returns an error when v is older than MinVersion or cannot be
// read. A pre-release counts as older than its release.
func checkVersion(v string) error {
	got, ok := parseVersion(v)
	if !ok {
		return fmt.Errorf("cannot read the backlog version %q", v)
	}
	minV, _ := parseVersion(MinVersion)
	if compareVersions(got, minV) < 0 {
		return fmt.Errorf("backlog %s is too old; Meat Mode needs Backlog.md %s or newer", v, MinVersion)
	}
	return nil
}

type version struct {
	nums [3]int
	pre  bool
}

func parseVersion(v string) (version, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	var out version
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		out.pre = v[i] == '-'
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out.nums[i] = n
	}
	return out, true
}

func compareVersions(a, b version) int {
	for i := range a.nums {
		if a.nums[i] != b.nums[i] {
			if a.nums[i] < b.nums[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case a.pre && !b.pre:
		return -1
	case !a.pre && b.pre:
		return 1
	}
	return 0
}
