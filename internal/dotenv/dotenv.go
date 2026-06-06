// Package dotenv is a minimal, dependency-free reader for .env files.
//
// It handles the common case: KEY=value lines, blank lines, and #-comment
// lines. Values may be wrapped in single or double quotes, which are stripped.
// It does NOT support trailing comments, ${VAR} expansion, escape sequences, or
// the "export" prefix.
package dotenv

import (
	"os"
	"strings"
)

// Load reads the given env file and loads it into the process environment.
//
// It WILL NOT override an environment variable that already exists — the file
// sets defaults.
func Load(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	currentEnv := map[string]bool{}
	for _, e := range os.Environ() {
		currentEnv[strings.SplitN(e, "=", 2)[0]] = true
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}

		val = stripQuotes(strings.TrimSpace(val))

		if !currentEnv[key] {
			_ = os.Setenv(key, val)
		}
	}

	return nil
}

// stripQuotes removes a single pair of matching surrounding single or double quotes.
func stripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
