package colors

import (
	"fmt"
	"strings"
)

// ParseOptionalHexColor parses an optional #RRGGBB color string.
func ParseOptionalHexColor(value string) (*int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}

	hexValue := strings.TrimPrefix(trimmed, "#")
	if len(hexValue) != 6 {
		return nil, fmt.Errorf("invalid color %q", value)
	}

	var parsed int
	for _, r := range hexValue {
		parsed <<= 4
		switch {
		case r >= '0' && r <= '9':
			parsed += int(r - '0')
		case r >= 'a' && r <= 'f':
			parsed += int(r-'a') + 10
		case r >= 'A' && r <= 'F':
			parsed += int(r-'A') + 10
		default:
			return nil, fmt.Errorf("invalid color %q", value)
		}
	}

	return &parsed, nil
}
