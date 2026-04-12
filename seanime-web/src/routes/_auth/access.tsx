import { AccessCodePage } from "@/app/(main)/_features/auth/access-code-page"
import { createFileRoute } from "@tanstack/react-router"

export const Route = createFileRoute("/_auth/access")({
    component: AccessCodePage,
})
