package secrets

import (
	"fmt"
	"os"
	"strings"
)

// LoadFromEnv reads an API key from environment variable.
// Supports comma-separated multi-key: "KEY1,KEY2" → returns first valid.
func LoadFromEnv(envVar string) (string, error) {
	if envVar == "" {
		return "", fmt.Errorf("api_key_env not set")
	}
	val := os.Getenv(envVar)
	if val == "" {
		return "", fmt.Errorf("environment variable %s is empty", envVar)
	}
	keys := strings.Split(val, ",")
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k != "" {
			return k, nil
		}
	}
	return "", fmt.Errorf("no valid key found in %s", envVar)
}
