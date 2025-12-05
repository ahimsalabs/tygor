package flavor

import (
	"fmt"
	"strconv"
	"strings"
)

// ValidateRule represents a single validation rule from a validate tag.
type ValidateRule struct {
	// Name is the rule name (e.g., "required", "email", "min").
	Name string

	// Param is the value after "=", empty if none.
	// For "min=8", Param is "8".
	Param string
}

// ZodSupport indicates how well a validator is supported.
type ZodSupport int

const (
	// ZodSupported means the validator has a direct Zod equivalent.
	ZodSupported ZodSupport = iota
	// ZodSkipped means the validator is intentionally skipped (handled structurally).
	ZodSkipped
	// ZodUnsupported means the validator has no Zod equivalent.
	ZodUnsupported
)

// ParseValidateTag parses a validate tag string into rules.
// Input: "required,email,min=8"
// Output: []ValidateRule{{Name:"required"}, {Name:"email"}, {Name:"min", Param:"8"}}
func ParseValidateTag(tag string) []ValidateRule {
	if tag == "" {
		return nil
	}

	parts := strings.Split(tag, ",")
	rules := make([]ValidateRule, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		rule := ValidateRule{}
		if idx := strings.Index(part, "="); idx > 0 {
			rule.Name = part[:idx]
			rule.Param = part[idx+1:]
		} else {
			rule.Name = part
		}
		rules = append(rules, rule)
	}

	return rules
}

// ZodMethod converts a ValidateRule to a Zod method chain.
// Returns empty string for unsupported rules.
// isString indicates if the field type is a string (affects min/max semantics).
func (r ValidateRule) ZodMethod(isString bool) string {
	method, _ := r.ZodMethodWithSupport(isString)
	return method
}

// ZodMethodWithSupport converts a ValidateRule to a Zod method chain and indicates support level.
// Returns (method, support) where method is empty for skipped/unsupported rules.
func (r ValidateRule) ZodMethodWithSupport(isString bool) (string, ZodSupport) {
	switch r.Name {
	case "required":
		if isString {
			return ".min(1)", ZodSupported
		}
		return "", ZodSupported // Non-string required is handled by not adding .optional()

	case "email":
		return ".email()", ZodSupported
	case "url":
		return ".url()", ZodSupported
	case "uuid":
		return ".uuid()", ZodSupported
	case "uri":
		return ".url()", ZodSupported

	case "min":
		if r.Param != "" {
			return fmt.Sprintf(".min(%s)", r.Param), ZodSupported
		}
	case "max":
		if r.Param != "" {
			return fmt.Sprintf(".max(%s)", r.Param), ZodSupported
		}
	case "len":
		if r.Param != "" {
			return fmt.Sprintf(".length(%s)", r.Param), ZodSupported
		}
	case "gt":
		if r.Param != "" {
			return fmt.Sprintf(".gt(%s)", r.Param), ZodSupported
		}
	case "gte":
		if r.Param != "" {
			return fmt.Sprintf(".gte(%s)", r.Param), ZodSupported
		}
	case "lt":
		if r.Param != "" {
			return fmt.Sprintf(".lt(%s)", r.Param), ZodSupported
		}
	case "lte":
		if r.Param != "" {
			return fmt.Sprintf(".lte(%s)", r.Param), ZodSupported
		}
	case "eq":
		if isString && r.Param != "" {
			return fmt.Sprintf(".refine(v => v === %q)", r.Param), ZodSupported
		}
		if r.Param != "" {
			return fmt.Sprintf(".refine(v => v === %s)", r.Param), ZodSupported
		}
	case "ne":
		if isString && r.Param != "" {
			return fmt.Sprintf(".refine(v => v !== %q)", r.Param), ZodSupported
		}
		if r.Param != "" {
			return fmt.Sprintf(".refine(v => v !== %s)", r.Param), ZodSupported
		}
	case "oneof":
		if r.Param != "" {
			values := strings.Fields(r.Param)
			if len(values) > 0 {
				quoted := make([]string, len(values))
				for i, v := range values {
					quoted[i] = fmt.Sprintf("%q", v)
				}
				return fmt.Sprintf("__ENUM__[%s]", strings.Join(quoted, ", ")), ZodSupported
			}
		}

	case "alphanum":
		return ".regex(/^[a-zA-Z0-9]+$/)", ZodSupported
	case "alpha":
		return ".regex(/^[a-zA-Z]+$/)", ZodSupported
	case "numeric":
		return ".regex(/^[0-9]+$/)", ZodSupported
	case "lowercase":
		return ".regex(/^[a-z]+$/)", ZodSupported
	case "uppercase":
		return ".regex(/^[A-Z]+$/)", ZodSupported

	case "contains":
		if r.Param != "" {
			return fmt.Sprintf(".includes(%q)", r.Param), ZodSupported
		}
	case "startswith":
		if r.Param != "" {
			return fmt.Sprintf(".startsWith(%q)", r.Param), ZodSupported
		}
	case "endswith":
		if r.Param != "" {
			return fmt.Sprintf(".endsWith(%q)", r.Param), ZodSupported
		}

	case "datetime":
		return ".datetime()", ZodSupported
	case "ip":
		return ".ip()", ZodSupported
	case "ip4", "ipv4":
		return ".ip({ version: \"v4\" })", ZodSupported
	case "ip6", "ipv6":
		return ".ip({ version: \"v6\" })", ZodSupported
	case "base64":
		return ".regex(/^[A-Za-z0-9+/]*={0,2}$/)", ZodSupported
	case "base64url":
		return ".regex(/^[A-Za-z0-9_-]*={0,2}$/)", ZodSupported
	case "json":
		return ".refine((v) => { try { JSON.parse(v); return true; } catch { return false; } }, { message: 'Invalid JSON' })", ZodSupported
	case "hostname", "fqdn":
		return ".regex(/^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\\.)*[a-zA-Z]{2,}$/)", ZodSupported
	case "mac":
		return ".regex(/^([0-9A-Fa-f]{2}[:-]){5}([0-9A-Fa-f]{2})$/)", ZodSupported
	case "semver":
		return ".regex(/^\\d+\\.\\d+\\.\\d+(-[a-zA-Z0-9.]+)?(\\+[a-zA-Z0-9.]+)?$/)", ZodSupported
	case "uuid4":
		return ".uuid()", ZodSupported
	case "boolean":
		return ".refine((v) => ['true', 'false', '1', '0'].includes(v.toLowerCase()), { message: 'Must be a boolean string' })", ZodSupported
	case "latitude":
		return ".refine((v) => { const n = parseFloat(v); return !isNaN(n) && n >= -90 && n <= 90; }, { message: 'Invalid latitude' })", ZodSupported
	case "longitude":
		return ".refine((v) => { const n = parseFloat(v); return !isNaN(n) && n >= -180 && n <= 180; }, { message: 'Invalid longitude' })", ZodSupported
	case "e164":
		return ".regex(/^\\+[1-9]\\d{1,14}$/)", ZodSupported
	case "isbn", "isbn10":
		return ".regex(/^(?:\\d[- ]*){9}[\\dXx]$/)", ZodSupported
	case "isbn13":
		return ".regex(/^(?:\\d[- ]*){13}$/)", ZodSupported

	// Skipped - handled structurally or not applicable to client-side validation
	case "omitempty", "omitzero", "omitnil", "dive", "keys", "endkeys", "unique",
		"required_with", "required_without", "required_if", "excluded_if", "excluded_unless",
		"eqfield", "nefield", "gtfield", "gtefield", "ltfield", "ltefield",
		"eqcsfield", "necsfield", "gtcsfield", "gtecsfield", "ltcsfield", "ltecsfield":
		return "", ZodSkipped
	}

	return "", ZodUnsupported
}

// HasRequired returns true if any rule is "required".
func HasRequired(rules []ValidateRule) bool {
	for _, r := range rules {
		if r.Name == "required" {
			return true
		}
	}
	return false
}

// ZodMiniTypeKind indicates the type category for validation.
type ZodMiniTypeKind int

const (
	ZodMiniTypeNumber ZodMiniTypeKind = iota
	ZodMiniTypeString
	ZodMiniTypeArray
)

// ZodMiniCheck converts a ValidateRule to a zod-mini check function.
// Returns empty string for rules that don't need checks.
// typeKind indicates the field type category (affects min/max semantics).
func (r ValidateRule) ZodMiniCheck(typeKind ZodMiniTypeKind) (string, ZodSupport) {
	switch r.Name {
	case "required":
		if typeKind == ZodMiniTypeString {
			return "z.minLength(1)", ZodSupported
		}
		return "", ZodSupported // Non-string required is handled by not adding optional

	case "email":
		return "z.email()", ZodSupported
	case "url":
		return "z.url()", ZodSupported
	case "uuid":
		return "z.uuid()", ZodSupported
	case "uri":
		return "z.url()", ZodSupported

	case "min":
		if r.Param != "" {
			if typeKind == ZodMiniTypeString || typeKind == ZodMiniTypeArray {
				return fmt.Sprintf("z.minLength(%s)", r.Param), ZodSupported
			}
			return fmt.Sprintf("z.gte(%s)", r.Param), ZodSupported
		}
	case "max":
		if r.Param != "" {
			if typeKind == ZodMiniTypeString || typeKind == ZodMiniTypeArray {
				return fmt.Sprintf("z.maxLength(%s)", r.Param), ZodSupported
			}
			return fmt.Sprintf("z.lte(%s)", r.Param), ZodSupported
		}
	case "len":
		if r.Param != "" {
			return fmt.Sprintf("z.length(%s)", r.Param), ZodSupported
		}
	case "gt":
		if r.Param != "" {
			return fmt.Sprintf("z.gt(%s)", r.Param), ZodSupported
		}
	case "gte":
		if r.Param != "" {
			return fmt.Sprintf("z.gte(%s)", r.Param), ZodSupported
		}
	case "lt":
		if r.Param != "" {
			return fmt.Sprintf("z.lt(%s)", r.Param), ZodSupported
		}
	case "lte":
		if r.Param != "" {
			return fmt.Sprintf("z.lte(%s)", r.Param), ZodSupported
		}
	case "eq":
		if typeKind == ZodMiniTypeString && r.Param != "" {
			return fmt.Sprintf("z.refine(v => v === %q)", r.Param), ZodSupported
		}
		if r.Param != "" {
			return fmt.Sprintf("z.refine(v => v === %s)", r.Param), ZodSupported
		}
	case "ne":
		if typeKind == ZodMiniTypeString && r.Param != "" {
			return fmt.Sprintf("z.refine(v => v !== %q)", r.Param), ZodSupported
		}
		if r.Param != "" {
			return fmt.Sprintf("z.refine(v => v !== %s)", r.Param), ZodSupported
		}
	case "oneof":
		if r.Param != "" {
			values := strings.Fields(r.Param)
			if len(values) > 0 {
				quoted := make([]string, len(values))
				for i, v := range values {
					quoted[i] = fmt.Sprintf("%q", v)
				}
				return fmt.Sprintf("__ENUM__[%s]", strings.Join(quoted, ", ")), ZodSupported
			}
		}

	case "alphanum":
		return "z.regex(/^[a-zA-Z0-9]+$/)", ZodSupported
	case "alpha":
		return "z.regex(/^[a-zA-Z]+$/)", ZodSupported
	case "numeric":
		return "z.regex(/^[0-9]+$/)", ZodSupported
	case "lowercase":
		return "z.regex(/^[a-z]+$/)", ZodSupported
	case "uppercase":
		return "z.regex(/^[A-Z]+$/)", ZodSupported

	case "contains":
		if r.Param != "" {
			return fmt.Sprintf("z.includes(%q)", r.Param), ZodSupported
		}
	case "startswith":
		if r.Param != "" {
			return fmt.Sprintf("z.startsWith(%q)", r.Param), ZodSupported
		}
	case "endswith":
		if r.Param != "" {
			return fmt.Sprintf("z.endsWith(%q)", r.Param), ZodSupported
		}

	case "datetime":
		return "z.iso.datetime()", ZodSupported
	case "ip":
		return "z.ip()", ZodSupported
	case "ip4", "ipv4":
		return "z.ipv4()", ZodSupported
	case "ip6", "ipv6":
		return "z.ipv6()", ZodSupported
	case "base64":
		return "z.regex(/^[A-Za-z0-9+/]*={0,2}$/)", ZodSupported
	case "base64url":
		return "z.regex(/^[A-Za-z0-9_-]*={0,2}$/)", ZodSupported
	case "json":
		return "z.refine((v) => { try { JSON.parse(v); return true; } catch { return false; } })", ZodSupported
	case "hostname", "fqdn":
		return "z.regex(/^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\\.)*[a-zA-Z]{2,}$/)", ZodSupported
	case "mac":
		return "z.regex(/^([0-9A-Fa-f]{2}[:-]){5}([0-9A-Fa-f]{2})$/)", ZodSupported
	case "semver":
		return "z.regex(/^\\d+\\.\\d+\\.\\d+(-[a-zA-Z0-9.]+)?(\\+[a-zA-Z0-9.]+)?$/)", ZodSupported
	case "uuid4":
		return "z.uuid()", ZodSupported
	case "e164":
		return "z.regex(/^\\+[1-9]\\d{1,14}$/)", ZodSupported
	case "isbn", "isbn10":
		return "z.regex(/^(?:\\d[- ]*){9}[\\dXx]$/)", ZodSupported
	case "isbn13":
		return "z.regex(/^(?:\\d[- ]*){13}$/)", ZodSupported

	// Skipped - handled structurally or not applicable to client-side validation
	case "omitempty", "omitzero", "omitnil", "dive", "keys", "endkeys", "unique",
		"required_with", "required_without", "required_if", "excluded_if", "excluded_unless",
		"eqfield", "nefield", "gtfield", "gtefield", "ltfield", "ltefield",
		"eqcsfield", "necsfield", "gtcsfield", "gtecsfield", "ltcsfield", "ltecsfield",
		"boolean", "latitude", "longitude":
		return "", ZodSkipped
	}

	return "", ZodUnsupported
}

// HasOneOf returns the oneof values if present, nil otherwise.
func HasOneOf(rules []ValidateRule) []string {
	for _, r := range rules {
		if r.Name == "oneof" && r.Param != "" {
			return strings.Fields(r.Param)
		}
	}
	return nil
}

// GetNumericConstraint extracts a numeric constraint by name.
func GetNumericConstraint(rules []ValidateRule, name string) (int64, bool) {
	for _, r := range rules {
		if r.Name == name && r.Param != "" {
			if v, err := strconv.ParseInt(r.Param, 10, 64); err == nil {
				return v, true
			}
		}
	}
	return 0, false
}
