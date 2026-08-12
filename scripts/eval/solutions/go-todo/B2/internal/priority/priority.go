package priority

var mapping = map[string]string{
	"高":     "high",
	"中":     "medium",
	"低":     "low",
	"high":   "high",
	"medium": "medium",
	"low":    "low",
}

// Normalize maps Chinese priority labels to English equivalents.
func Normalize(p string) string {
	if v, ok := mapping[p]; ok {
		return v
	}
	return p
}
