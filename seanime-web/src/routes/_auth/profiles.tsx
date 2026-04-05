import { ProfilePickerPage } from "@/app/(main)/_features/auth/profile-picker-page"
import { createFileRoute } from "@tanstack/react-router"

export const Route = createFileRoute("/_auth/profiles")({
    component: ProfilePickerPage,
})
