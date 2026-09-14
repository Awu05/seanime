import Page from "@/app/(main)/activity/page"
import { createLazyFileRoute } from "@tanstack/react-router"

export const Route = createLazyFileRoute("/_main/activity/")({
    component: Page,
})
