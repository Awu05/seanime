import { useClearOfflineCopies, useGetOfflineCopies, useSetImageCacheLimit } from "@/api/hooks/filecache.hooks"
import { ConfirmationDialog, useConfirmationDialog } from "@/components/shared/confirmation-dialog"
import { Button } from "@/components/ui/button"
import { NumberInput } from "@/components/ui/number-input"
import React from "react"
import { SettingsCard } from "../_components/settings-card"

const MIN_IMAGE_CACHE_MB = 100
const MAX_IMAGE_CACHE_MB = 1048576 // 1 TB, mirrors imagecache.MaxMaxMB

export function OfflineCopiesSettings() {
    const { data } = useGetOfflineCopies()
    const { mutate: setLimit, isPending: isSaving } = useSetImageCacheLimit()
    const { mutate: clear, isPending: isClearing } = useClearOfflineCopies()

    const [limit, setLimitValue] = React.useState<number>()
    React.useEffect(() => {
        if (data) setLimitValue(data.imageCacheMaxMB)
    }, [data?.imageCacheMaxMB])

    const confirmClear = useConfirmationDialog({
        title: "Clear saved title info and images",
        description: "They'll be saved again as people browse. Until then, cleared items won't show during an internet outage.",
        actionText: "Clear",
        actionIntent: "alert",
        onConfirm: () => clear(),
    })

    const canSave = !!limit && limit >= MIN_IMAGE_CACHE_MB && limit <= MAX_IMAGE_CACHE_MB && limit !== data?.imageCacheMaxMB

    return (
        <SettingsCard
            title="Offline copies"
            description="Episode info and images saved so the library stays complete during an internet outage. Shared by all profiles."
        >
            <p className="text-sm text-[--muted]">Using {data?.totalSize ?? "…"}</p>

            <div className="flex gap-2 items-end flex-wrap">
                <NumberInput
                    label="Image cache limit (MB)"
                    value={limit}
                    min={MIN_IMAGE_CACHE_MB}
                    max={MAX_IMAGE_CACHE_MB}
                    onValueChange={value => setLimitValue(value)}
                    className="max-w-[200px]"
                />
                <Button intent="white-subtle" disabled={!canSave || isSaving} onClick={() => setLimit({ imageCacheMaxMB: limit! })}>
                    Save
                </Button>
            </div>

            <Button intent="warning-subtle" onClick={confirmClear.open} disabled={isClearing}>
                Clear saved title info and images
            </Button>
            <ConfirmationDialog {...confirmClear} />
        </SettingsCard>
    )
}
