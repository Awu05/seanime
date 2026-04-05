# Phase 6: Frontend Multi-User UX — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Profile picker UI, admin panel for profile management, profile-scoped settings, and nav bar profile indicator — all platform-aware (Docker vs Electron).

**Architecture:** Auth pages (login, access code, setup, profile picker) were created in Phase 1. This phase adds: admin profile management UI in settings, profile indicator in the nav bar with switch/logout, and platform-aware routing that checks `serverStatus.isDesktopSidecar` to determine which auth flow to show.

**Tech Stack:** React, TanStack Router, Jotai, Tailwind CSS

---

## File Structure

### New Files

| File | Responsibility |
|------|---------------|
| `seanime-web/src/app/(main)/settings/_containers/profile-management-settings.tsx` | Admin UI: create/delete profiles, set access code |
| `seanime-web/src/app/(main)/_features/navigation/profile-indicator.tsx` | Nav bar profile avatar with switch/logout dropdown |
| `seanime-web/src/app/(main)/_atoms/profile.atoms.ts` | Jotai atoms for current profile state |
| `seanime-web/src/api/hooks/profile-management.hooks.ts` | TanStack Query hooks for admin profile CRUD |

### Modified Files

| File | Change |
|------|--------|
| `seanime-web/src/app/(main)/_features/navigation/main-sidebar.tsx` | Add profile indicator component |
| `seanime-web/src/app/(main)/settings/page.tsx` | Add profile management tab (admin only) |
| `seanime-web/src/app/(main)/server-data-wrapper.tsx` | Add multi-user auth redirect logic |
| `seanime-web/src/api/generated/endpoints.ts` | Add profile management endpoint definitions |

---

## Tasks

### Task 1: Profile Atoms

**Files:**
- Create: `seanime-web/src/app/(main)/_atoms/profile.atoms.ts`

- [ ] **Step 1: Create profile state atoms**

```typescript
import { atom } from "jotai"
import { atomWithStorage } from "jotai/utils"

export type ProfileInfo = {
    id: string
    name: string
    isAdmin: boolean
    avatar?: string
}

// Current profile — set after profile selection, cleared on logout
export const currentProfileAtom = atomWithStorage<ProfileInfo | null>(
    "sea-current-profile",
    null,
)

// Whether multi-user mode is active
export const multiUserEnabledAtom = atom<boolean>(false)
```

- [ ] **Step 2: Commit**

```bash
git add seanime-web/src/app/(main)/_atoms/profile.atoms.ts
git commit -m "feat: add profile state atoms"
```

---

### Task 2: Profile Management API Hooks

**Files:**
- Create: `seanime-web/src/api/hooks/profile-management.hooks.ts`
- Modify: `seanime-web/src/api/generated/endpoints.ts`

- [ ] **Step 1: Add profile management endpoints to API_ENDPOINTS**

In the `AUTH` section of `API_ENDPOINTS`, the `CreateProfile` and `SetAccessCode` endpoints were already added in Phase 1. Add a `DeleteProfile` endpoint if not present:

```typescript
        DeleteProfile: {
            key: "AUTH-delete-profile",
            methods: ["DELETE"],
            endpoint: "/api/v1/admin/profiles",
        },
        UpdateProfilePin: {
            key: "AUTH-update-profile-pin",
            methods: ["POST"],
            endpoint: "/api/v1/profiles",
        },
```

- [ ] **Step 2: Create profile management hooks**

Create `seanime-web/src/api/hooks/profile-management.hooks.ts`:

```typescript
import { API_ENDPOINTS } from "@/api/generated/endpoints"
import { useServerMutation } from "@/api/client/requests"

export function useCreateProfile() {
    return useServerMutation<
        any,
        { name: string; avatar?: string; pin?: string }
    >({
        endpoint: API_ENDPOINTS.AUTH.CreateProfile.endpoint,
        method: API_ENDPOINTS.AUTH.CreateProfile.methods[0],
        mutationKey: [API_ENDPOINTS.AUTH.CreateProfile.key],
    })
}

export function useDeleteProfile() {
    return useServerMutation<
        { success: boolean },
        { id: string }
    >({
        endpoint: API_ENDPOINTS.AUTH.DeleteProfile.endpoint,
        method: "DELETE",
        mutationKey: [API_ENDPOINTS.AUTH.DeleteProfile.key],
    })
}

export function useSetInstanceAccessCode() {
    return useServerMutation<
        { success: boolean },
        { accessCode: string }
    >({
        endpoint: API_ENDPOINTS.AUTH.SetAccessCode.endpoint,
        method: API_ENDPOINTS.AUTH.SetAccessCode.methods[0],
        mutationKey: [API_ENDPOINTS.AUTH.SetAccessCode.key],
    })
}

export function useUpdateProfilePin() {
    return useServerMutation<
        { success: boolean },
        { id: string; pin: string }
    >({
        endpoint: API_ENDPOINTS.AUTH.UpdateProfilePin.endpoint + "/${id}/pin",
        method: "POST",
        mutationKey: [API_ENDPOINTS.AUTH.UpdateProfilePin.key],
    })
}
```

- [ ] **Step 3: Commit**

```bash
git add seanime-web/src/api/generated/endpoints.ts seanime-web/src/api/hooks/profile-management.hooks.ts
git commit -m "feat: add profile management API hooks"
```

---

### Task 3: Profile Management Settings Page

**Files:**
- Create: `seanime-web/src/app/(main)/settings/_containers/profile-management-settings.tsx`

- [ ] **Step 1: Create profile management UI**

Create `seanime-web/src/app/(main)/settings/_containers/profile-management-settings.tsx`:

```typescript
import { useAuthGetProfiles } from "@/api/hooks/auth.hooks"
import { useCreateProfile, useDeleteProfile, useSetInstanceAccessCode } from "@/api/hooks/profile-management.hooks"
import { useQueryClient } from "@tanstack/react-query"
import { API_ENDPOINTS } from "@/api/generated/endpoints"
import React from "react"
import { toast } from "sonner"

export function ProfileManagementSettings() {
    const qc = useQueryClient()
    const { data: profiles } = useAuthGetProfiles()
    const { mutate: createProfile, isPending: isCreating } = useCreateProfile()
    const { mutate: deleteProfile } = useDeleteProfile()
    const { mutate: setAccessCode } = useSetInstanceAccessCode()

    const [newName, setNewName] = React.useState("")
    const [newAccessCode, setNewAccessCode] = React.useState("")

    function handleCreateProfile(e: React.FormEvent) {
        e.preventDefault()
        if (!newName.trim()) return
        createProfile({ name: newName.trim() }, {
            onSuccess: () => {
                setNewName("")
                qc.invalidateQueries({ queryKey: [API_ENDPOINTS.AUTH.GetProfiles.key] })
                toast.success("Profile created")
            },
            onError: () => toast.error("Failed to create profile"),
        })
    }

    function handleDeleteProfile(id: string, name: string) {
        if (!confirm(`Delete profile "${name}"?`)) return
        deleteProfile({ id }, {
            onSuccess: () => {
                qc.invalidateQueries({ queryKey: [API_ENDPOINTS.AUTH.GetProfiles.key] })
                toast.success("Profile deleted")
            },
            onError: () => toast.error("Failed to delete profile"),
        })
    }

    function handleSetAccessCode(e: React.FormEvent) {
        e.preventDefault()
        setAccessCode({ accessCode: newAccessCode }, {
            onSuccess: () => {
                setNewAccessCode("")
                toast.success("Access code updated")
            },
            onError: () => toast.error("Failed to update access code"),
        })
    }

    return (
        <div className="space-y-6">
            <div>
                <h3 className="text-lg font-semibold text-white mb-4">Profiles</h3>
                <div className="space-y-2">
                    {profiles?.map((profile: any) => (
                        <div key={profile.id} className="flex items-center justify-between p-3 bg-gray-900 rounded-lg border border-gray-800">
                            <div className="flex items-center gap-3">
                                <div className="w-10 h-10 rounded-full bg-gradient-to-br from-brand-500 to-brand-700 flex items-center justify-center text-white font-bold">
                                    {profile.name?.[0]?.toUpperCase()}
                                </div>
                                <div>
                                    <p className="font-medium text-white">{profile.name}</p>
                                    {profile.isAdmin && <span className="text-xs text-brand-400">Admin</span>}
                                </div>
                            </div>
                            {!profile.isAdmin && (
                                <button
                                    onClick={() => handleDeleteProfile(profile.id, profile.name)}
                                    className="text-sm text-red-400 hover:text-red-300"
                                >
                                    Delete
                                </button>
                            )}
                        </div>
                    ))}
                </div>

                <form onSubmit={handleCreateProfile} className="flex gap-2 mt-4">
                    <input
                        type="text"
                        value={newName}
                        onChange={e => setNewName(e.target.value)}
                        placeholder="New profile name"
                        className="flex-1 px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-white"
                    />
                    <button
                        type="submit"
                        disabled={isCreating || !newName.trim()}
                        className="px-4 py-2 bg-brand-500 hover:bg-brand-600 text-white rounded-lg disabled:opacity-50"
                    >
                        Add
                    </button>
                </form>
            </div>

            <div>
                <h3 className="text-lg font-semibold text-white mb-4">Instance Access Code</h3>
                <p className="text-sm text-gray-400 mb-2">Household members enter this code to access the profile picker.</p>
                <form onSubmit={handleSetAccessCode} className="flex gap-2">
                    <input
                        type="text"
                        value={newAccessCode}
                        onChange={e => setNewAccessCode(e.target.value)}
                        placeholder="New access code"
                        className="flex-1 px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-white"
                    />
                    <button
                        type="submit"
                        className="px-4 py-2 bg-brand-500 hover:bg-brand-600 text-white rounded-lg"
                    >
                        Update
                    </button>
                </form>
            </div>
        </div>
    )
}
```

- [ ] **Step 2: Commit**

```bash
git add seanime-web/src/app/(main)/settings/_containers/profile-management-settings.tsx
git commit -m "feat: add profile management settings UI"
```

---

### Task 4: Profile Indicator in Nav Bar

**Files:**
- Create: `seanime-web/src/app/(main)/_features/navigation/profile-indicator.tsx`

- [ ] **Step 1: Create profile indicator component**

Create `seanime-web/src/app/(main)/_features/navigation/profile-indicator.tsx`:

```typescript
import { useAuthLogout } from "@/api/hooks/auth.hooks"
import { currentProfileAtom } from "@/app/(main)/_atoms/profile.atoms"
import { useAtom } from "jotai"
import React from "react"

export function ProfileIndicator() {
    const [profile, setProfile] = useAtom(currentProfileAtom)
    const { mutate: logout } = useAuthLogout()
    const [open, setOpen] = React.useState(false)

    if (!profile) return null

    function handleSwitchProfile() {
        setProfile(null)
        window.location.href = "/profiles"
    }

    function handleLogout() {
        logout(undefined, {
            onSuccess: () => {
                setProfile(null)
                window.location.href = "/login"
            },
        })
    }

    return (
        <div className="relative">
            <button
                onClick={() => setOpen(!open)}
                className="flex items-center gap-2 px-2 py-1 rounded-lg hover:bg-gray-800 transition-colors"
            >
                <div className="w-7 h-7 rounded-full bg-gradient-to-br from-brand-500 to-brand-700 flex items-center justify-center text-white text-xs font-bold">
                    {profile.name?.[0]?.toUpperCase()}
                </div>
                <span className="text-sm text-gray-300 hidden md:inline">{profile.name}</span>
            </button>

            {open && (
                <>
                    <div className="fixed inset-0 z-40" onClick={() => setOpen(false)} />
                    <div className="absolute bottom-full left-0 mb-2 w-48 bg-gray-900 border border-gray-700 rounded-lg shadow-xl z-50 py-1">
                        <button
                            onClick={handleSwitchProfile}
                            className="w-full text-left px-4 py-2 text-sm text-gray-300 hover:bg-gray-800"
                        >
                            Switch Profile
                        </button>
                        <button
                            onClick={handleLogout}
                            className="w-full text-left px-4 py-2 text-sm text-red-400 hover:bg-gray-800"
                        >
                            Logout
                        </button>
                    </div>
                </>
            )}
        </div>
    )
}
```

- [ ] **Step 2: Commit**

```bash
git add seanime-web/src/app/(main)/_features/navigation/profile-indicator.tsx
git commit -m "feat: add profile indicator component for nav bar"
```

---

### Task 5: Integrate Profile Indicator in Main Sidebar

**Files:**
- Modify: `seanime-web/src/app/(main)/_features/navigation/main-sidebar.tsx`

- [ ] **Step 1: Add ProfileIndicator to the sidebar**

Read `main-sidebar.tsx` first. Find the `SidebarFooter` or bottom section. Import and render `ProfileIndicator`:

```typescript
import { ProfileIndicator } from "./profile-indicator"
```

Add `<ProfileIndicator />` in the sidebar footer area, before the settings link.

- [ ] **Step 2: Commit**

```bash
git add seanime-web/src/app/(main)/_features/navigation/main-sidebar.tsx
git commit -m "feat: add profile indicator to main sidebar"
```

---

### Task 6: Add Profile Management Tab to Settings

**Files:**
- Modify: `seanime-web/src/app/(main)/settings/page.tsx`

- [ ] **Step 1: Add profile management section**

Read `settings/page.tsx`. Add a new tab/section for "Profile Management" that renders `ProfileManagementSettings`. Gate it behind admin check — only show if the current user is admin.

Import:
```typescript
import { ProfileManagementSettings } from "./_containers/profile-management-settings"
```

Add the component in the appropriate location within the settings tabs, gated by admin status.

- [ ] **Step 2: Verify frontend build**

```bash
wsl -d Ubuntu -e bash -c "cd /mnt/c/Users/awu05/OneDrive/Documents/Github/seanime/seanime-web && npx rsbuild build 2>&1" | tail -5
```

- [ ] **Step 3: Commit**

```bash
git add seanime-web/src/app/(main)/settings/page.tsx
git commit -m "feat: add profile management tab to settings page"
```

---

### Task 7: Verify Full Build

- [ ] **Step 1: Build frontend**

```bash
wsl -d Ubuntu -e bash -c "cd /mnt/c/Users/awu05/OneDrive/Documents/Github/seanime/seanime-web && npm run build 2>&1" | tail -10
```

Expected: No errors
