package handlers

import (
	"seanime/internal/database/models"
	"testing"
)

// TestContinuityEnabled documents why the watch-continuity gate can't simply trust
// ContinuityManager's cached flag in multi-user mode: InitOrRefreshModules ignores the
// profileID it's given and always reloads the global/admin settings row, so the cached flag
// never reflects a non-admin profile's own "Enable Watch Continuity" toggle. The gate must
// instead read that profile's independent settings row directly.
func TestContinuityEnabled(t *testing.T) {
	tests := []struct {
		name              string
		multiUserEnabled  bool
		profileID         string
		profileSettings   *models.Settings
		globalFlagEnabled bool
		want              bool
	}{
		{
			name:              "single-user mode falls back to the global flag",
			multiUserEnabled:  false,
			profileID:         "",
			profileSettings:   nil,
			globalFlagEnabled: true,
			want:              true,
		},
		{
			name:              "non-admin profile with continuity enabled on their own row",
			multiUserEnabled:  true,
			profileID:         "profile-b",
			profileSettings:   &models.Settings{Library: &models.LibrarySettings{EnableWatchContinuity: true}},
			globalFlagEnabled: false,
			want:              true,
		},
		{
			name:              "non-admin profile with continuity disabled on their own row, even though the global/admin flag is on",
			multiUserEnabled:  true,
			profileID:         "profile-b",
			profileSettings:   &models.Settings{Library: &models.LibrarySettings{EnableWatchContinuity: false}},
			globalFlagEnabled: true,
			want:              false,
		},
		{
			name:              "non-admin profile with no settings row yet defaults to disabled",
			multiUserEnabled:  true,
			profileID:         "profile-b",
			profileSettings:   nil,
			globalFlagEnabled: true,
			want:              false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := continuityEnabled(tt.multiUserEnabled, tt.profileID, tt.profileSettings, tt.globalFlagEnabled)
			if got != tt.want {
				t.Errorf("continuityEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
