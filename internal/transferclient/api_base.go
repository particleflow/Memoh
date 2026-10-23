package transferclient

import (
	"net/url"
	"strconv"
	"strings"
)

// ValidateAPIBase checks the configured HTTPS API prefix without resolving DNS,
// reading credentials or normalizing a persisted host binding. Private network
// names/addresses are allowed as explicit configuration; TLS still validates
// the configured hostname. This is not endpoint discovery or authorization.
func ValidateAPIBase(base string) error {
	_, err := parseAPIBase(base)
	return err
}

func parseAPIBase(base string) (*url.URL, error) {
	// Reject ambiguous routing before appending any task/file namespace. Query
	// and fragment delimiters count even when empty. Escaped path separators,
	// dot segments and backslashes can be interpreted differently by gateways.
	if len(base) == 0 || len(base) > 2048 || strings.ContainsAny(base, "?#%\\") {
		return nil, ErrInvalid
	}
	for _, c := range base {
		if c <= ' ' || c == 127 {
			return nil, ErrInvalid
		}
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Opaque != "" || strings.HasSuffix(u.Host, ":") {
		return nil, ErrInvalid
	}
	if port := u.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return nil, ErrInvalid
		}
	}
	if strings.Contains(u.Path, "//") {
		return nil, ErrInvalid
	}
	path := strings.TrimSuffix(u.Path, "/") // Preserve existing single trailing slash compatibility.
	for _, segment := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if segment == "." || segment == ".." {
			return nil, ErrInvalid
		}
		for _, c := range segment {
			allowed := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~", c)
			if !allowed {
				return nil, ErrInvalid
			}
		}
	}
	return u, nil
}
