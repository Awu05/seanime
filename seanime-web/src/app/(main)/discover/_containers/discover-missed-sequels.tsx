import { useAnilistListMissedSequels } from "@/api/hooks/anilist.hooks"
import { MediaCardGrid } from "@/app/(main)/_features/media/_components/media-card-grid"
import { MediaEntryCard } from "@/app/(main)/_features/media/_components/media-entry-card"
import { MediaEntryCardSkeleton } from "@/app/(main)/_features/media/_components/media-entry-card-skeleton"
import { DiscoverRowHeader } from "@/app/(main)/discover/_components/discover-row-header"
import { PageWrapper } from "@/components/shared/page-wrapper"
import { Carousel, CarouselContent, CarouselDotButtons } from "@/components/ui/carousel"
import { useInView } from "motion/react"
import React from "react"


export function DiscoverMissedSequelsSection({ title = "You Might Have Missed" }: { title?: string }) {
    const ref = React.useRef(null)
    const isInView = useInView(ref, { once: true })
    const { data, isLoading } = useAnilistListMissedSequels(isInView)

    if (!isInView && !data) return <div ref={ref} />

    if (!data?.length) return null

    return (
        <PageWrapper className="space-y-2 z-[5] relative" data-discover-missed-sequels-container>
            <DiscoverRowHeader title={title}>
                <MediaCardGrid maxCol={5}>
                    {data.map(media => <MediaEntryCard key={media.id} media={media} showLibraryBadge showTrailer showPreviewButton type="anime" />)}
                </MediaCardGrid>
            </DiscoverRowHeader>
            <Carousel
                className="w-full max-w-full"
                gap="xl"
                opts={{
                    align: "start",
                    dragFree: true,
                }}
                autoScroll
            >
                {/*<CarouselMasks />*/}
                <CarouselDotButtons />
                <CarouselContent className="px-6" ref={ref}>
                    {!isLoading ? data?.filter(Boolean).map(media => {
                        return (
                            <MediaEntryCard
                                key={media.id}
                                media={media}
                                showLibraryBadge
                                containerClassName="basis-[200px] md:basis-[250px] mx-2 mt-8 mb-0"
                                showTrailer
                                type="anime"
                            />
                        )
                    }) : [...Array(10).keys()].map((v, idx) => <MediaEntryCardSkeleton key={idx} />)}
                </CarouselContent>
            </Carousel>
        </PageWrapper>
    )

}
