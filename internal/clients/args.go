package clients

import (
	"slices"
	"strings"
)

// MergeArgs removes generated options explicitly supplied by the caller before
// appending passArgs unchanged. defaults must contain only options, with values
// either joined by '=' or following options named in valueOptions. aliases maps native
// alternative spellings to the generated spelling; an empty mapping marks a
// valueless short option that can precede a value option in a cluster.
// additive explicitly names native repeatable value options: retain their generated entries unless the
// caller supplies the same value. Repeated caller options remain unchanged.
// This is not a full native argument parser: flag-shaped separate caller values
// may still be mistaken for options. Use --option=value for such literal values.
func MergeArgs(defaults, passArgs []string, aliases map[string]string, valueOptions []string, additive ...string) []string {
	provided := make(map[string][]string)
	for i, arg := range passArgs {
		if arg == "--" {
			break
		}
		name, value, joined := strings.Cut(arg, "=")
		if canonical, ok := aliases[name]; ok {
			name = canonical
		} else if strings.HasPrefix(name, "-") && !strings.HasPrefix(name, "--") && len(name) > 2 {
			// Skip only known valueless short options. Stop at the first value
			// option (including an unknown one), so -pMODEL is never read as -m.
			for j := 1; j < len(name); j++ {
				canonical, ok := aliases["-"+name[j:j+1]]
				if !ok {
					break
				}
				if canonical != "" {
					name = canonical
					break
				}
			}
		}
		if slices.Contains(additive, name) && !joined && i+1 < len(passArgs) && passArgs[i+1] != "--" {
			value = passArgs[i+1]
		}
		provided[name] = append(provided[name], value)
	}
	args := make([]string, 0, len(defaults)+len(passArgs))
	for i := 0; i < len(defaults); {
		start := i
		name, value, joined := strings.Cut(defaults[i], "=")
		i++
		if !joined && slices.Contains(valueOptions, name) && i < len(defaults) {
			value = defaults[i]
			i++
		}
		values, overridden := provided[name]
		if slices.Contains(additive, name) {
			overridden = slices.Contains(values, value)
		}
		if !overridden {
			args = append(args, defaults[start:i]...)
		}
	}
	return append(args, passArgs...)
}
