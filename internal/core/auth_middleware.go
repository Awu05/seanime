package core

func GetProfileIDFromContext(c interface{ Get(string) interface{} }) string {
	v := c.Get("profileId")
	if v == nil {
		return ""
	}
	return v.(string)
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
