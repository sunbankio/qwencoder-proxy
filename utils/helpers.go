package utils

import (
	"fmt"
	"strings"
)

func FormatIntWithCommas(n int64) string {
	if n == 0 {
		return "0"
	}

	var result []string
	s := fmt.Sprintf("%d", n)
	sign := ""

	if s[0] == '-' {
		sign = "-"
		s = s[1:]
	}

	for i, c := range s {
		if (len(s)-i)%3 == 0 && i != 0 {
			result = append(result, ",")
		}
		result = append(result, string(c))
	}

	return sign + strings.Join(result, "")
}