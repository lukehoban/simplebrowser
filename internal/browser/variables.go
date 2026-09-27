package browser

import "strings"

// An invalid variable is carried through the cascade so a lower-priority
// declaration does not incorrectly take its place.
const invalidVariable = "\x00invalid-var"

func containsVarFunction(text string) bool {
	return strings.Contains(strings.ToLower(text), "var(")
}

// substituteVars expands token streams before interpreting property syntax.
// Strings are opaque, and commas inside nested functions are not fallbacks.
// The bounded recursion also protects the renderer from adversarial CSS.
func substituteVars(text string, properties ComputedStyle, visiting map[string]bool) (string, bool) {
	value, ok, _ := substituteVarsInner(text, properties, visiting)
	return value, ok
}

func substituteVarsInner(text string, properties ComputedStyle, visiting map[string]bool) (string, bool, bool) {
	if len(text) > 1<<20 || len(visiting) > 128 {
		return "", false, false
	}
	var out strings.Builder
	for i := 0; i < len(text); {
		if text[i] == '"' || text[i] == '\'' {
			start, quote := i, text[i]
			i++
			for i < len(text) {
				if text[i] == '\\' && i+1 < len(text) {
					i += 2
				} else if text[i] == quote {
					i++
					break
				} else {
					i++
				}
			}
			out.WriteString(text[start:i])
			continue
		}
		if i+4 > len(text) || !strings.EqualFold(text[i:i+4], "var(") ||
			(i > 0 && cssIdent(text[i-1])) {
			out.WriteByte(text[i])
			i++
			continue
		}
		start, depth, quote := i+4, 1, byte(0)
		j := start
		comma := -1
		for ; j < len(text) && depth > 0; j++ {
			c := text[j]
			if quote != 0 {
				if c == '\\' && j+1 < len(text) {
					j++
				} else if c == quote {
					quote = 0
				}
				continue
			}
			switch c {
			case '\'', '"':
				quote = c
			case '(':
				depth++
			case ')':
				depth--
			case ',':
				if depth == 1 && comma == -1 {
					comma = j
				}
			}
		}
		if depth != 0 {
			return "", false, false
		}
		end := j - 1
		nameEnd := end
		if comma >= 0 {
			nameEnd = comma
		}
		name := strings.TrimSpace(text[start:nameEnd])
		if !strings.HasPrefix(name, "--") || !validProperty(name) {
			return "", false, false
		}
		// A dependency cycle makes the entire custom property's computed
		// value invalid. A fallback inside that cycle cannot repair it.
		if visiting[name] {
			return "", false, true
		}
		raw, exists := properties[name]
		if raw == invalidVariable {
			exists = false
		}
		var replacement string
		var ok bool
		var cyclic bool
		if exists {
			if visiting == nil {
				visiting = make(map[string]bool)
			}
			visiting[name] = true
			replacement, ok, cyclic = substituteVarsInner(raw, properties, visiting)
			delete(visiting, name)
		}
		if cyclic {
			return "", false, true
		}
		if !exists || !ok {
			if comma < 0 {
				return "", false, false
			}
			replacement, ok, cyclic = substituteVarsInner(text[comma+1:end], properties, visiting)
			if !ok {
				return "", false, cyclic
			}
		}
		out.WriteString(replacement)
		i = j
	}
	if out.Len() > 1<<20 {
		return "", false, false
	}
	return out.String(), true, false
}
