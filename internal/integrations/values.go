package integrations

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"timone/internal/model"
)

var credentialText = regexp.MustCompile(`(?i)(-----BEGIN [A-Z ]*PRIVATE KEY|bearer\s+[a-z0-9._~+/-]+|(?:password|passwd|token|secret|api[_-]?key|credential)\s*[:=])`)

func sensitiveKey(key string) bool {
	key = strings.ToLower(key)
	key = strings.NewReplacer("_", "", "-", "", ".", "", "/", "").Replace(key)
	for _, word := range []string{"password", "passwd", "secret", "token", "credential", "privatekey", "apikey", "authorization", "cookie", "connectionstring", "certificate", "clientkey", "tlskey", "accesskey", "connectionuri", "dsn", "pwd", "passphrase", "signingkey", "encryptionkey", "sshkey", "jwt"} {
		if strings.Contains(key, word) {
			return true
		}
	}
	return key == "key" || key == "auth" || key == "env" || key == "environment" || key == "extraenv" || key == "envvars" || key == "data" || key == "binarydata" || key == "stringdata" || key == "command" || key == "args"
}

// Conservative automatic sanitization, not a general secret-discovery guarantee.
// Only this sanitized copy can enter comparisons; caller must discard raw input.
func SanitizeValues(input model.Object) (model.Object, error) {
	count := 0
	var walk func(any, string, int) (any, error)
	walk = func(value any, path string, depth int) (any, error) {
		count++
		if count > 10000 || depth > 32 {
			return nil, fmt.Errorf("Values exceed safe structural bounds")
		}
		if sensitiveKey(path) {
			return "[REDACTED]", nil
		}
		switch v := value.(type) {
		case map[string]any:
			result := map[string]any{}
			keys := []string{}
			for key := range v {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			namedSensitive := sensitiveKey(model.Str(v, "name"))
			for _, key := range keys {
				if namedSensitive && (key == "value" || key == "values") {
					result[key] = "[REDACTED]"
					continue
				}
				next, err := walk(v[key], key, depth+1)
				if err != nil {
					return nil, err
				}
				result[key] = next
			}
			return result, nil
		case model.Object:
			return walk(map[string]any(v), path, depth)
		case []any:
			result := []any{}
			for _, item := range v {
				next, err := walk(item, path, depth+1)
				if err != nil {
					return nil, err
				}
				result = append(result, next)
			}
			return result, nil
		case string:
			if len(v) > 4096 || credentialText.MatchString(v) {
				return "[REDACTED]", nil
			}
			parsed, err := url.Parse(v)
			if err == nil && (parsed.User != nil || model.SensitivePath(parsed.RawQuery)) {
				return "[REDACTED]", nil
			}
			if err == nil {
				for key := range parsed.Query() {
					if sensitiveKey(key) {
						return "[REDACTED]", nil
					}
				}
			}
			return v, nil
		default:
			return value, nil
		}
	}
	if input == nil {
		return model.Object{}, nil
	}
	out, err := walk(map[string]any(input), "", 0)
	if err != nil {
		return nil, err
	}
	result, _ := out.(map[string]any)
	return model.Object(result), nil
}
