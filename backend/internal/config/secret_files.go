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
func ResolveSecretFiles(lookup LookupFunc, readFile func(string) ([]byte, error), names ...string) (LookupFunc, error) {
	resolved := make(map[string]string, len(names))
	for _, name := range names {
		path, ok := lookup(name + "_FILE")
		if !ok {
			continue
		}
		content, err := readFile(path)
		if err != nil {
			return nil, fmt.Errorf("config: reading %s_FILE: %w", name, err)
		}
		resolved[name] = strings.TrimSuffix(string(content), "\n")
	}

	return func(key string) (string, bool) {
		if v, ok := resolved[key]; ok {
			return v, true
		}
		return lookup(key)
	}, nil
}
