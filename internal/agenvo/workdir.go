package agenvo

import (
	"fmt"
	"strings"
)

// resolveWorkDir uses the current authorized Windows target root, independently
// of the server OS/cwd and the pane's prior cwd. This is lexical path validation,
// not a filesystem sandbox: junctions, symlinks and Bash retain OS permissions.
func resolveWorkDir(root, requested string) (string, error) {
	invalid := fmt.Errorf("work_dir must stay within the configured Windows target root; use . or a relative child path: %w", ErrUnsupported)
	volume, rootParts, ok := absoluteWindowsParts(root)
	if !ok {
		return "", invalid
	}
	requested = strings.ReplaceAll(requested, "/", `\`)
	var relative []string
	if windowsAbsolute.MatchString(requested) {
		otherVolume, parts, valid := absoluteWindowsParts(requested)
		if !valid || !strings.EqualFold(volume, otherVolume) || len(parts) < len(rootParts) {
			return "", invalid
		}
		for i, part := range rootParts {
			if !strings.EqualFold(part, parts[i]) {
				return "", invalid
			}
		}
		relative = parts[len(rootParts):]
	} else {
		// A leading separator is drive-relative; a colon is drive-relative or ADS.
		if strings.HasPrefix(requested, `\`) {
			return "", invalid
		}
		var valid bool
		relative, valid = cleanWindowsParts(strings.Split(requested, `\`))
		if !valid {
			return "", invalid
		}
	}
	resolved := volume + strings.Join(append(rootParts, relative...), `\`)
	if len(resolved) > 4096 {
		return "", invalid
	}
	return resolved, nil
}

func absoluteWindowsParts(value string) (string, []string, bool) {
	value = strings.ReplaceAll(value, "/", `\`)
	if len(value) > 4096 || !windowsAbsolute.MatchString(value) {
		return "", nil, false
	}
	var volume, tail string
	if strings.HasPrefix(value, `\\`) {
		parts := strings.Split(value[2:], `\`)
		if len(parts) < 2 || !windowsPathComponent(parts[0]) || !windowsPathComponent(parts[1]) {
			return "", nil, false
		}
		volume = `\\` + parts[0] + `\` + parts[1] + `\`
		tail = strings.Join(parts[2:], `\`)
	} else {
		volume, tail = value[:3], value[3:]
	}
	parts, ok := cleanWindowsParts(strings.Split(tail, `\`))
	return volume, parts, ok
}

func cleanWindowsParts(parts []string) ([]string, bool) {
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			if len(clean) == 0 {
				return nil, false
			}
			clean = clean[:len(clean)-1]
		default:
			if !windowsPathComponent(part) {
				return nil, false
			}
			clean = append(clean, part)
		}
	}
	return clean, true
}

func windowsPathComponent(part string) bool {
	return part != "" && part != "." && part != ".." && !strings.HasSuffix(part, ".") && !strings.HasSuffix(part, " ") && !strings.ContainsAny(part, ":<>\"|?*") && strings.IndexFunc(part, func(r rune) bool { return r < 32 }) < 0
}
