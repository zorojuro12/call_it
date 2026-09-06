// Package toolchain provides pure parsers for the repository's Go toolchain
// pins — go.mod's `go` directive and CI's `go-version` pin — plus the floor
// this project holds them to. No I/O: callers supply file contents as
// strings.
package toolchain

import (
	"fmt"
	"strings"
)

// MinGo is the major.minor Go version floor this repository holds.
const MinGo = "1.26"

// ParseGoDirective returns the version string from a go.mod's `go` line,
// e.g. "1.26.7". A trailing "// comment" on the directive line is stripped.
// A commented-out directive (the whole line starts with "//") does not
// count.
func ParseGoDirective(gomod string) (string, error) {
	for _, line := range strings.Split(gomod, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		if idx := strings.Index(trimmed, "//"); idx >= 0 {
			trimmed = strings.TrimSpace(trimmed[:idx])
		}
		fields := strings.Fields(trimmed)
		if len(fields) >= 2 && fields[0] == "go" {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("no go directive found")
}

// ParseCIPin returns every `go-version:` value found in a GitHub Actions
// workflow file, in file order, with surrounding quotes and any trailing
// comment stripped.
func ParseCIPin(workflow string) ([]string, error) {
	var pins []string
	for _, line := range strings.Split(workflow, "\n") {
		trimmed := strings.TrimSpace(line)
		trimmed = strings.TrimPrefix(trimmed, "- ")
		if !strings.HasPrefix(trimmed, "go-version:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, "go-version:"))
		if idx := strings.Index(value, "#"); idx >= 0 {
			value = strings.TrimSpace(value[:idx])
		}
		value = strings.Trim(value, `"'`)
		pins = append(pins, value)
	}
	if len(pins) == 0 {
		return nil, fmt.Errorf("no go-version pin found")
	}
	return pins, nil
}

// MajorMinor returns the first two dot-separated components of a version
// string, e.g. "1.26.7" -> "1.26". A version with fewer than two components
// is returned unchanged.
func MajorMinor(version string) string {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return version
	}
	return parts[0] + "." + parts[1]
}
