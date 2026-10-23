package config

import (
	"fmt"
	"strings"
)

// ResolveSecretFiles lets each named variable be supplied as a file: when
// NAME_FILE is set, NAME is read from the file it names, minus one
// trailing newline. Production mounts secrets as files (deploy/secrets.sh,
// compose.prod.yml) because a container's environment is persisted in
// Docker's on-disk container config; a file bind-mounted from tmpfs is
// not. Every file is read once, here, so a bad one fails at startup.
//
// Setting both NAME and NAME_FILE is an error rather than a precedence
// rule, so a leftover variable can't silently win. Errors name the file's
// path, never its content.
func ResolveSecretFiles(lookup LookupFunc, readFile func(string) ([]byte, error), names ...string) (LookupFunc, error) {
	resolved := make(map[string]string, len(names))
	for _, name := range names {
		path, ok := lookup(name + "_FILE")
		if !ok {
			continue
		}
		if _, both := lookup(name); both {
			return nil, fmt.Errorf("config: set %s or %s_FILE, not both", name, name)
		}
		if path == "" {
			return nil, fmt.Errorf("config: %s_FILE must not be empty", name)
		}
		content, err := readFile(path)
		if err != nil {
			return nil, fmt.Errorf("config: reading %s_FILE: %w", name, err)
		}
		value := strings.TrimSuffix(string(content), "\n")
		if value == "" {
			return nil, fmt.Errorf("config: %s_FILE %s is empty", name, path)
		}
		resolved[name] = value
	}

	return func(key string) (string, bool) {
		if v, ok := resolved[key]; ok {
			return v, true
		}
		return lookup(key)
	}, nil
}
