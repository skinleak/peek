package scan

import (
	"cmp"
	"os/user"
	"slices"
	"strconv"
)

// cachedUserLookup resolves uids to user names, falling back to the numeric
// uid when the user is unknown.
func cachedUserLookup() func(int) string {
	cache := make(map[int]string)
	return func(uid int) string {
		if name, ok := cache[uid]; ok {
			return name
		}
		name := strconv.Itoa(uid)
		if u, err := user.LookupId(name); err == nil {
			name = u.Username
		}
		cache[uid] = name
		return name
	}
}

func sortListeners(ls []Listener) {
	slices.SortFunc(ls, func(a, b Listener) int {
		return cmp.Or(
			cmp.Compare(a.Port, b.Port),
			a.Address.Compare(b.Address),
			cmp.Compare(a.PID, b.PID),
		)
	})
}
