package core

func GetProfileIDFromContext(c interface{ Get(string) interface{} }) string {
	v := c.Get("profileId")
	if v == nil {
		return ""
	}
	return v.(string)
}

// NormalizeSimklProfileID maps the empty profileID (single-user/sidecar mode) to the "_default"
// sentinel used everywhere SIMKL account/settings rows and the sync worker's default-profile
// resolvers key their lookups - GetProfileIDFromContext returns "" for that case, never "_default",
// so every SIMKL DB read/write keyed on a request's profileID must go through this first.
func NormalizeSimklProfileID(profileID string) string {
	if profileID == "" {
		return DefaultSimklProfileID
	}
	return profileID
}

func GetIsAdminFromContext(c interface{ Get(string) interface{} }) bool {
	v := c.Get("isAdmin")
	if v == nil {
		return false
	}
	return v.(bool)
}

func GetAuthScopeFromContext(c interface{ Get(string) interface{} }) string {
	v := c.Get("authScope")
	if v == nil {
		return ""
	}
	return v.(string)
}
