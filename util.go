package venstar

import (
	"regexp"
	"strings"
)

var camelCaseRegex = regexp.MustCompile(`([A-Z]+)`)

func toSnakeCase(camelCase string) string {
	s := camelCaseRegex.ReplaceAllStringFunc(camelCase, func(c string) string {
		return "_"+strings.ToLower(c)
	})
	return strings.TrimPrefix(s, "_")
}
