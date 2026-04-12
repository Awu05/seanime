import { LoginPage } from "@/app/(main)/_features/auth/login-page"
import { createFileRoute } from "@tanstack/react-router"

export const Route = createFileRoute("/_auth/login")({
    component: LoginPage,
})
