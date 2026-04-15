package plugin

import "github.com/dop251/goja"

// getProfileIDFromVM reads the __profileID variable from the Goja runtime,
// which is set by the hook executor when a hook event carries a profile ID.
func getProfileIDFromVM(vm *goja.Runtime) string {
	v := vm.Get("__profileID")
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return ""
	}
	return v.String()
}
