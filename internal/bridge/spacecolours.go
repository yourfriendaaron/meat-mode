package bridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// SpaceColoursName is the file in the state folder that holds the workspace
// colours when MEAT_MODE_SPACE_COLOURS is not set.
const SpaceColoursName = "space-colours.json"

// maxSpaceColours is the most of the file the bridge reads.
const maxSpaceColours = 1 << 20

// SpaceColour is one kept entry of the workspace colours file: a folder, as
// the bridge resolves repo folders, and the list colour slot (0 to 5) the
// user gave its workspace.
type SpaceColour struct {
	Path string
	Slot int
}

// spaceNames maps the colour names the file may use to list colour slots.
var spaceNames = map[string]int{
	"ansi5": 0, "ansi6": 1, "ansi2": 2, "ansi3": 3, "ansi4": 4, "ansi1": 5,
}

// spaceRefs are the reference colours of slots 0 to 5 that a picked colour
// is matched against: Herdr's own space colours (docs/api.md, Workspace
// colours), not the app's --role-repo-* tokens.
var spaceRefs = [6][3]int{
	{0x8b, 0x7c, 0xf6}, {0x22, 0xa9, 0x9a}, {0x3f, 0xae, 0x6a},
	{0xe3, 0x81, 0x4f}, {0x5b, 0x8d, 0xef}, {0xd9, 0x64, 0x9a},
}

// spaceSlot is the slot of a colour value from the file, or false when the
// value is none of the six names and not `#` and six hex digits.
func spaceSlot(v string) (int, bool) {
	if s, ok := spaceNames[v]; ok {
		return s, true
	}
	if len(v) != 7 || v[0] != '#' {
		return 0, false
	}
	var rgb [3]int
	for i := range rgb {
		n := 0
		for _, c := range v[1+2*i : 3+2*i] {
			switch {
			case c >= '0' && c <= '9':
				n = n*16 + int(c-'0')
			case c >= 'a' && c <= 'f':
				n = n*16 + int(c-'a') + 10
			case c >= 'A' && c <= 'F':
				n = n*16 + int(c-'A') + 10
			default:
				return 0, false
			}
		}
		rgb[i] = n
	}
	best, bestD := 0, -1
	for slot, ref := range spaceRefs {
		d := 0
		for i := range rgb {
			d += (rgb[i] - ref[i]) * (rgb[i] - ref[i])
		}
		if bestD < 0 || d < bestD { // a tie keeps the lower slot
			best, bestD = slot, d
		}
	}
	return best, true
}

// SpaceColoursPath is where the workspace colours file is: the absolute path
// in MEAT_MODE_SPACE_COLOURS, else space-colours.json in the state folder.
// A relative variable, or no state folder with the variable unset, is an
// error.
func SpaceColoursPath(getenv func(string) string, state string) (string, error) {
	if v := getenv("MEAT_MODE_SPACE_COLOURS"); v != "" {
		if !filepath.IsAbs(v) {
			return "", fmt.Errorf("MEAT_MODE_SPACE_COLOURS must be an absolute path, not %q", v)
		}
		return v, nil
	}
	if state == "" {
		return "", errors.New("no home folder, so no place for " + SpaceColoursName + "; set MEAT_MODE_SPACE_COLOURS")
	}
	return filepath.Join(state, SpaceColoursName), nil
}

// LoadSpaceColours returns the loader for Bridge.LoadSpaceColours: it finds
// the file as SpaceColoursPath does on each call, so the variable and the
// file are read afresh each time.
func LoadSpaceColours(getenv func(string) string, state string) func() ([]SpaceColour, error) {
	return func() ([]SpaceColour, error) {
		path, err := SpaceColoursPath(getenv, state)
		if err != nil {
			return nil, err
		}
		return ReadSpaceColours(path)
	}
}

// ReadSpaceColours reads the workspace colours file at path and returns its
// kept entries in file order. A missing file gives none and no error. An
// unreadable file, one that is not a regular file, one over 1 MiB, or one that is not a JSON object whose
// version is the number 1 gives an error, and no entries. An entry whose
// path or colour breaks the rules is skipped.
func ReadSpaceColours(path string) ([]SpaceColour, error) {
	// Not blocking, so a pipe or device at the path cannot hang the bridge.
	data, err := tracker.ReadRegular(path, maxSpaceColours)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read the workspace colours file %s: %w", path, err)
	}
	if len(data) > maxSpaceColours {
		return nil, fmt.Errorf("the workspace colours file %s is over 1 MiB", path)
	}
	return parseSpaceColours(path, data)
}

func parseSpaceColours(path string, data []byte) ([]SpaceColour, error) {
	var top map[string]json.RawMessage
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) || json.Unmarshal(data, &top) != nil {
		return nil, fmt.Errorf("the workspace colours file %s is not a JSON object", path)
	}
	var version float64
	if json.Unmarshal(top["version"], &version) != nil || version != 1 {
		return nil, fmt.Errorf("the workspace colours file %s does not have version 1", path)
	}
	var spaces []json.RawMessage
	if raw := top["spaces"]; raw != nil && json.Unmarshal(raw, &spaces) != nil {
		return nil, fmt.Errorf("the workspace colours file %s has spaces that is not a list", path)
	}
	var out []SpaceColour
	for _, raw := range spaces {
		var e struct{ Path, Colour *string }
		if json.Unmarshal(raw, &e) != nil || e.Path == nil || e.Colour == nil {
			continue
		}
		slot, ok := spaceSlot(*e.Colour)
		if !ok || !filepath.IsAbs(*e.Path) {
			continue
		}
		out = append(out, SpaceColour{Path: resolvePath(filepath.Clean(*e.Path)), Slot: slot})
	}
	return out, nil
}

// holdsPath reports whether dir is path or holds it by whole segments.
func holdsPath(dir, path string) bool {
	return dir == path || strings.HasPrefix(path, strings.TrimSuffix(dir, "/")+"/")
}

// assignSpaceColours gives each repo folder in dirs the slot of the entry
// that reaches it, or nil. An entry reaches the deepest repo whose folder is
// its path or holds it. For one repo an entry at its own folder beats one
// inside it, and the first in the file wins among equals.
func assignSpaceColours(entries []SpaceColour, dirs []string) []*int {
	out := make([]*int, len(dirs))
	exact := make([]bool, len(dirs))
	for _, e := range entries {
		target := -1
		for i, dir := range dirs {
			if holdsPath(dir, e.Path) && (target < 0 || len(dir) > len(dirs[target])) {
				target = i
			}
		}
		if target < 0 {
			continue
		}
		isExact := dirs[target] == e.Path
		if out[target] == nil || (isExact && !exact[target]) {
			slot := e.Slot
			out[target], exact[target] = &slot, isExact
		}
	}
	return out
}
