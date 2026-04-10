import { useGetMissingEpisodes } from "@/api/hooks/anime_entries.hooks"
import { useServerStatus } from "@/app/(main)/_hooks/use-server-status"
import { CustomLibraryBanner } from "@/app/(main)/_features/anime-library/_containers/custom-library-banner"
import { PluginWebviewSlot } from "@/app/(main)/_features/plugin/webview/plugin-webviews"
import { MissingEpisodes } from "@/app/(main)/schedule/_components/missing-episodes"
import { UpcomingEpisodes } from "@/app/(main)/schedule/_containers/upcoming-episodes.tsx"
import { PageWrapper } from "@/components/shared/page-wrapper"
import { AppLayoutStack } from "@/components/ui/app-layout"
import { LoadingSpinner } from "@/components/ui/loading-spinner"
import { Switch } from "@/components/ui/switch"
import { useAtom } from "jotai"
import { atomWithStorage } from "jotai/utils"
import React from "react"
import { ScheduleCalendar } from "./_components/schedule-calendar"

const showAllAiringAtom = atomWithStorage<boolean>("sea-schedule-show-all-airing", false)


export default function Page() {

    const { data, isLoading } = useGetMissingEpisodes()
    const serverStatus = useServerStatus()
    const isAuthenticated = !!serverStatus?.user && !serverStatus?.user?.isSimulated

    const [showAllStored, setShowAll] = useAtom(showAllAiringAtom)
    // Default to true when not authenticated, otherwise use stored preference
    const showAll = !isAuthenticated ? true : showAllStored

    if (isLoading) return <LoadingSpinner />

    return (
        <>
            <CustomLibraryBanner discrete />
            <PageWrapper
                className="p-4 sm:p-8 space-y-10 pb-10"
            >
                <PluginWebviewSlot slot="schedule-screen-top" />
                <MissingEpisodes data={data} isLoading={isLoading} />
                <UpcomingEpisodes />
                <AppLayoutStack>

                    <div className="hidden lg:flex items-center justify-between">
                        <div className="space-y-2">
                            <h2>Release schedule</h2>
                            <p className="text-[--muted]">{showAll ? "All currently airing anime" : "Based on your anime list"}</p>
                        </div>
                        <Switch
                            label="Show all airing"
                            side="left"
                            value={showAll}
                            onValueChange={setShowAll}
                        />
                    </div>
                    <div className="lg:hidden flex items-center justify-between">
                        <Switch
                            label="Show all airing"
                            side="left"
                            value={showAll}
                            onValueChange={setShowAll}
                        />
                    </div>

                    <ScheduleCalendar showAll={showAll} />
                </AppLayoutStack>
                <PluginWebviewSlot slot="schedule-screen-bottom" />
            </PageWrapper>
        </>
    )
}
