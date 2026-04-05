import { atom } from "jotai"
import { atomWithStorage } from "jotai/utils"

export type ProfileInfo = {
    id: string
    name: string
    isAdmin: boolean
    avatar?: string
}

export const currentProfileAtom = atomWithStorage<ProfileInfo | null>(
    "sea-current-profile",
    null,
)

export const multiUserEnabledAtom = atom<boolean>(false)
