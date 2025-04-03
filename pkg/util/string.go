package util

import (
	"slices"
	"strings"
)

// MapToEnvString converts a map[string]string to KEY=value format
func MapToEnvString(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}

	pairs := make([]string, 0, len(m))
	for key, value := range m {
		pairs = append(pairs, key+"="+value)
	}

	slices.Sort(pairs)

	return strings.Join(pairs, " ")
}
