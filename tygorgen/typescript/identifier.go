package typescript

import (
	"strings"
	"unicode"
)

// TypeScript reserved words from Appendix B.
var reservedWords = map[string]bool{
	"break":      true,
	"case":       true,
	"catch":      true,
	"class":      true,
	"const":      true,
	"continue":   true,
	"debugger":   true,
	"default":    true,
	"delete":     true,
	"do":         true,
	"else":       true,
	"enum":       true,
	"export":     true,
	"extends":    true,
	"false":      true,
	"finally":    true,
	"for":        true,
	"function":   true,
	"if":         true,
	"implements": true,
	"import":     true,
	"in":         true,
	"instanceof": true,
	"interface":  true,
	"let":        true,
	"new":        true,
	"null":       true,
	"package":    true,
	"private":    true,
	"protected":  true,
	"public":     true,
	"return":     true,
	"static":     true,
	"super":      true,
	"switch":     true,
	"this":       true,
	"throw":      true,
	"true":       true,
	"try":        true,
	"type":       true,
	"typeof":     true,
	"var":        true,
	"void":       true,
	"while":      true,
	"with":       true,
	"yield":      true,
}

// escapeReservedWord escapes a reserved word by appending an underscore.
func escapeReservedWord(name string) string {
	if reservedWords[name] {
		return name + "_"
	}
	return name
}

// needsQuoting returns true if an identifier needs to be quoted.
func needsQuoting(name string) bool {
	if name == "" {
		return true
	}

	// Check if it starts with a number
	if unicode.IsDigit(rune(name[0])) {
		return true
	}

	// Check for invalid characters
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '$' {
			return true
		}
	}

	// Check for reserved words
	if reservedWords[name] {
		return true
	}

	return false
}

// sanitizeIdentifier makes an identifier valid for TypeScript.
func sanitizeIdentifier(name string) string {
	if name == "" {
		return "_"
	}

	var result strings.Builder

	// Handle leading digit
	if unicode.IsDigit(rune(name[0])) {
		result.WriteRune('_')
	}

	// Replace invalid characters with underscores
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '$' {
			result.WriteRune(r)
		} else {
			result.WriteRune('_')
		}
	}

	sanitized := result.String()

	// Escape reserved words
	return escapeReservedWord(sanitized)
}
