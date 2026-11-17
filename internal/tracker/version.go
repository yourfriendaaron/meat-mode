package tracker

import (
	"strconv"
	"strings"
)

type version struct {
	nums [3]int
	pre  bool
}

// CompareVersion compares the version string v, as a tool prints it, with
// min: -1 when v is older, 0 when equal, 1 when newer. A pre-release counts
// as older than its release. ok is false when v or min is not a version.
func CompareVersion(v, min string) (cmp int, ok bool) {
	a, ok := parseVersion(v)
	if !ok {
		return 0, false
	}
	b, ok := parseVersion(min)
	if !ok {
		return 0, false
	}
	for i := range a.nums {
		if a.nums[i] != b.nums[i] {
			if a.nums[i] < b.nums[i] {
				return -1, true
			}
			return 1, true
		}
	}
	switch {
	case a.pre && !b.pre:
		return -1, true
	case !a.pre && b.pre:
		return 1, true
	}
	return 0, true
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
