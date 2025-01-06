package formatting

import "strings"

func Anonymize(s string) string {
	visibleChars := 4
	if len(s) <= visibleChars {
		return strings.Repeat("*", len(s))
	}
	//If you want to show the total lenght, uncomment the following line
	//return s[:visibleChars] + strings.Repeat("*", len(s)-visibleChars)
	return s[:visibleChars] + strings.Repeat("*", 6)
}
