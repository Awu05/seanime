import { SetupPage } from "@/app/(main)/_features/auth/setup-page"
import { createFileRoute } from "@tanstack/react-router"

export const Route = createFileRoute("/_auth/setup")({
    component: SetupPage,
})
