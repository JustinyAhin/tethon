package resource

import (
	"path/filepath"
	"strings"
	"unicode"
)

const (
	DefaultCommandMaxBytes = 240
	DefaultPathMaxBytes    = 160
)

type RedactedString struct {
	Value     string
	Redacted  bool
	Truncated bool
}

type PathRoot struct {
	Label string
	Path  string
}

type RedactionOptions struct {
	MaxBytes  int
	PathRoots []PathRoot
}

func RedactCommandLine(raw string, opts RedactionOptions) RedactedString {
	maxBytes := opts.MaxBytes
	if maxBytes == 0 {
		maxBytes = DefaultCommandMaxBytes
	}

	fields := splitCommandLine(raw)
	redacted := false
	pendingSecretValue := false
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if pendingSecretValue {
			out = append(out, "[redacted]")
			redacted = true
			pendingSecretValue = false
			continue
		}
		if isSensitiveFlagWithoutValue(field) {
			out = append(out, field)
			pendingSecretValue = true
			redacted = true
			continue
		}
		value, changed := redactCommandField(field, opts.PathRoots)
		if changed {
			redacted = true
		}
		out = append(out, value)
	}

	value, truncated := boundString(strings.Join(out, " "), maxBytes)
	return RedactedString{Value: value, Redacted: redacted, Truncated: truncated}
}

func RedactPath(raw string, roots []PathRoot, maxBytes int) RedactedString {
	if maxBytes == 0 {
		maxBytes = DefaultPathMaxBytes
	}
	value := strings.TrimSpace(strings.Trim(raw, `"'`))
	if value == "" {
		return RedactedString{}
	}
	if isSensitivePath(value) {
		return RedactedString{Value: "[redacted path]", Redacted: true}
	}

	clean := filepath.Clean(value)
	for _, root := range roots {
		rootPath := strings.TrimSpace(root.Path)
		if rootPath == "" {
			continue
		}
		rootClean := filepath.Clean(rootPath)
		if !pathWithin(clean, rootClean) {
			continue
		}
		label := strings.TrimPrefix(strings.TrimSpace(root.Label), "$")
		if label == "" {
			label = "owned"
		}
		display := "$" + label
		if rel, err := filepath.Rel(rootClean, clean); err == nil && rel != "." {
			display += "/" + filepath.ToSlash(rel)
		}
		value, truncated := boundString(display, maxBytes)
		return RedactedString{Value: value, Redacted: display != raw, Truncated: truncated}
	}

	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "~/") {
		display := "[external]/" + filepath.Base(clean)
		value, truncated := boundString(display, maxBytes)
		return RedactedString{Value: value, Redacted: true, Truncated: truncated}
	}

	value, truncated := boundString(filepath.ToSlash(clean), maxBytes)
	return RedactedString{Value: value, Truncated: truncated}
}

func redactCommandField(field string, roots []PathRoot) (string, bool) {
	if isEnvAssignment(field) {
		return "[env]", true
	}
	if key, value, ok := strings.Cut(field, "="); ok {
		if isSensitiveName(key) {
			return key + "=[redacted]", true
		}
		if looksLikePath(value) {
			path := RedactPath(value, roots, DefaultPathMaxBytes)
			return key + "=" + path.Value, path.Redacted || path.Truncated
		}
	}
	if isSensitiveInlineValue(field) {
		return "[redacted]", true
	}
	if looksLikePath(field) {
		path := RedactPath(field, roots, DefaultPathMaxBytes)
		return path.Value, path.Redacted || path.Truncated
	}
	return field, false
}

func splitCommandLine(raw string) []string {
	fields := []string{}
	var b strings.Builder
	var quote rune
	inField := false
	escaped := false
	for _, r := range raw {
		if escaped {
			b.WriteRune(r)
			inField = true
			escaped = false
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			inField = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			b.WriteRune(r)
			inField = true
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			inField = true
			continue
		}
		if unicode.IsSpace(r) {
			if inField {
				fields = append(fields, b.String())
				b.Reset()
				inField = false
			}
			continue
		}
		b.WriteRune(r)
		inField = true
	}
	if inField {
		fields = append(fields, b.String())
	}
	return fields
}

func isEnvAssignment(field string) bool {
	key, value, ok := strings.Cut(field, "=")
	if !ok || key == "" || value == "" || strings.HasPrefix(key, "-") {
		return false
	}
	for i, r := range key {
		if i == 0 {
			if r != '_' && !unicode.IsLetter(r) {
				return false
			}
			continue
		}
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func isSensitiveFlagWithoutValue(field string) bool {
	if strings.Contains(field, "=") || !strings.HasPrefix(field, "-") {
		return false
	}
	return isSensitiveName(field)
}

func isSensitiveName(name string) bool {
	normalized := strings.TrimLeft(strings.ToLower(name), "-")
	normalized = strings.NewReplacer("-", "_", ".", "_").Replace(normalized)
	for _, marker := range []string{"api_key", "apikey", "authorization", "cookie", "password", "passwd", "secret", "token"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func isSensitiveInlineValue(value string) bool {
	normalized := strings.ToLower(value)
	for _, marker := range []string{"authorization:", "bearer ", "cookie:", "api_key=", "apikey=", "password=", "secret=", "token="} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func looksLikePath(value string) bool {
	return filepath.IsAbs(value) || strings.HasPrefix(value, "~/") || strings.Contains(value, "/")
}

func isSensitivePath(value string) bool {
	normalized := strings.ToLower(filepath.ToSlash(value))
	base := filepath.Base(normalized)
	if base == ".env" || strings.HasPrefix(base, ".env.") || strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key") {
		return true
	}
	for _, marker := range []string{"/.env", "/.ssh/", "/id_rsa", "/id_ed25519", "/credentials", "/secrets"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func pathWithin(path, root string) bool {
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func boundString(value string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value, false
	}
	suffix := " [truncated]"
	if maxBytes <= len(suffix) {
		return value[:maxBytes], true
	}
	return value[:maxBytes-len(suffix)] + suffix, true
}
