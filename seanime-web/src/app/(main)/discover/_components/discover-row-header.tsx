import { Button } from "@/components/ui/button"
import { Modal } from "@/components/ui/modal"
import React from "react"

// A Discover row's title with a "View all" button. The modal's content only mounts while it's open,
// so its queries don't run until someone asks for the full list.
export function DiscoverRowHeader({ title, children }: { title: string, children: React.ReactNode }) {
    const [open, setOpen] = React.useState(false)

    return (
        <>
            <div className="flex items-center gap-3" data-discover-row-header>
                <h2>{title}</h2>
                <Button intent="gray-link" size="sm" onClick={() => setOpen(true)}>View all</Button>
            </div>
            <Modal open={open} onOpenChange={setOpen} title={title} contentClass="max-w-7xl">
                {children}
            </Modal>
        </>
    )
}
