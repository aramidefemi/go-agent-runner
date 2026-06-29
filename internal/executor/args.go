package executor

import (
	"sort"
	"strings"
)

func ExpandArgs(args []string, vars map[string]string) []string {
	expanded := make([]string, 0, len(args))
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, arg := range args {
		out := arg
		for _, key := range keys {
			out = strings.ReplaceAll(out, "{{"+key+"}}", vars[key])
		}
		expanded = append(expanded, out)
	}
	return expanded
}
