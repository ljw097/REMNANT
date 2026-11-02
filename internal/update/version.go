package update

import (
	"fmt"
	"regexp"
	"strconv"
)

var versionRe = regexp.MustCompile(`^v(0|[1-9]\d{0,5})\.(0|[1-9]\d{0,5})\.(0|[1-9]\d{0,5})(?:-([0-9A-Za-z.-]{1,40}))?$`)

// Version is a parsed "vMAJOR.MINOR.PATCH[-PRERELEASE]".
type Version struct {
	Major, Minor, Patch int
	Pre                 string
	raw                 string
}

func ParseVersion(s string) (Version, error) {
	m := versionRe.FindStringSubmatch(s)
	if m == nil {
		return Version{}, fmt.Errorf("update: %q is not a release version (vX.Y.Z)", s)
	}
	v := Version{Pre: m[4], raw: s}
	v.Major, _ = strconv.Atoi(m[1])
	v.Minor, _ = strconv.Atoi(m[2])
	v.Patch, _ = strconv.Atoi(m[3])
	return v, nil
}

func (v Version) String() string { return v.raw }

// Compare returns -1, 0 or 1. A pre-release sorts before its release;
// pre-releases of the same version compare as strings.
func (v Version) Compare(o Version) int {
	for _, d := range [][2]int{{v.Major, o.Major}, {v.Minor, o.Minor}, {v.Patch, o.Patch}} {
		if d[0] != d[1] {
			if d[0] < d[1] {
				return -1
			}
			return 1
		}
	}
	switch {
	case v.Pre == o.Pre:
		return 0
	case v.Pre == "":
		return 1
	case o.Pre == "":
		return -1
	case v.Pre < o.Pre:
		return -1
	}
	return 1
}
