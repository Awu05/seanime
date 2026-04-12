import { createFileRoute, Outlet } from "@tanstack/react-router"

export const Route = createFileRoute("/_auth")({
    component: AuthLayout,
})

function AuthLayout() {
    return (
        <div className="min-h-screen flex items-center justify-center bg-gray-950">
            <div className="w-full max-w-md p-8">
                <Outlet />
            </div>
        </div>
    )
}
