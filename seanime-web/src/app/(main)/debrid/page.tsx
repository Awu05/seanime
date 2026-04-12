import { useServerMutation } from "@/api/client/requests"
import { Debrid_TorrentItem } from "@/api/generated/types"
import {
    useDebridCancelDownload,
    useDebridDeleteLocalDownload,
    useDebridDeleteTorrent,
    useDebridDownloadTorrent,
    useDebridGetLocalDownloads,
    useDebridGetTorrents,
} from "@/api/hooks/debrid.hooks"
import { CustomLibraryBanner } from "@/app/(main)/_features/anime-library/_containers/custom-library-banner"
import { useWebsocketMessageListener } from "@/app/(main)/_hooks/handle-websockets"
import { useLibraryPathSelection } from "@/app/(main)/_hooks/use-library-path-selection"
import { useServerStatus } from "@/app/(main)/_hooks/use-server-status"
import { ConfirmationDialog, useConfirmationDialog } from "@/components/shared/confirmation-dialog"
import { DirectorySelector } from "@/components/shared/directory-selector"
import { LuffyError } from "@/components/shared/luffy-error"
import { PageWrapper } from "@/components/shared/page-wrapper"
import { SeaLink } from "@/components/shared/sea-link"
import { AppLayoutStack } from "@/components/ui/app-layout"
import { Button, IconButton } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { cn } from "@/components/ui/core/styling"
import { LoadingSpinner } from "@/components/ui/loading-spinner"
import { Modal } from "@/components/ui/modal"
import { Tooltip } from "@/components/ui/tooltip"
import { clientIdAtom } from "@/app/websocket-provider"
import { WSEvents } from "@/lib/server/ws-events"
import { useQueryClient } from "@tanstack/react-query"
import { formatDate } from "date-fns"
import { atom } from "jotai"
import { useAtom, useAtomValue } from "jotai/react"
import capitalize from "lodash/capitalize"
import React from "react"
import { BiDownArrow, BiLinkExternal, BiRefresh, BiTime, BiTrash, BiX } from "react-icons/bi"
import { FaCheckCircle } from "react-icons/fa"
import { FcFolder } from "react-icons/fc"
import { FiDownload } from "react-icons/fi"
import { HiFolderDownload } from "react-icons/hi"
import { IoPlayCircle } from "react-icons/io5"
import { toast } from "sonner"


function getServiceName(provider: string) {
    switch (provider) {
        case "realdebrid":
            return "Real-Debrid"
        case "torbox":
            return "TorBox"
        case "alldebrid":
            return "AllDebrid"
        case "stremthru":
            return "StremThru"
        default:
            return provider
    }
}

function getDashboardLink(provider: string) {
    switch (provider) {
        case "torbox":
            return "https://torbox.app/dashboard"
        case "realdebrid":
            return "https://real-debrid.com/torrents"
        case "alldebrid":
            return "https://alldebrid.com/magnets/"
        default:
            return ""
    }
}

export default function Page() {
    const serverStatus = useServerStatus()

    if (!serverStatus) return <LoadingSpinner />

    if (!serverStatus?.debridSettings?.enabled || !serverStatus?.debridSettings?.provider) return <LuffyError
        title="Debrid not enabled"
    >
        Debrid service is not enabled or configured
    </LuffyError>

    return (
        <>
            <CustomLibraryBanner discrete />
            <PageWrapper
                className="space-y-4 p-4 sm:p-8"
            >
                <Content />
            </PageWrapper>
            <TorrentItemModal />
            <PlayTorrentModal />
            <PlayChoiceModal />
            <DeleteChoiceModal />
        </>
    )
}

function Content() {
    const serverStatus = useServerStatus()
    const qc = useQueryClient()
    const [enabled, setEnabled] = React.useState(true)
    const [refetchInterval, setRefetchInterval] = React.useState(30000)

    const { data, isLoading, status, refetch } = useDebridGetTorrents(enabled, refetchInterval)
    const { data: localDownloads } = useDebridGetLocalDownloads()

    // Refresh the local-downloads list when a download completes so the badge appears
    // without needing a manual refresh.
    useWebsocketMessageListener<{ status: string }>({
        type: WSEvents.DEBRID_DOWNLOAD_PROGRESS,
        onMessage: data => {
            if (data?.status === "completed") {
                qc.invalidateQueries({ queryKey: ["debrid-get-local-downloads"] })
            }
        },
    })

    // Set of debrid torrent ids that have a local download record
    const downloadedTorrentIds = React.useMemo(() => {
        return new Set((localDownloads ?? []).map(d => d.torrentItemId))
    }, [localDownloads])

    React.useEffect(() => {
        const hasDownloads = data?.filter(t => t.status === "downloading" || t.status === "paused")?.length ?? 0
        setRefetchInterval(hasDownloads ? 5000 : 30000)
    }, [data])

    React.useEffect(() => {
        if (status === "error") {
            setEnabled(false)
        }
    }, [status])

    if (!enabled) return <LuffyError title="Failed to connect">
        <div className="flex flex-col gap-4 items-center">
            <p className="max-w-md">Failed to connect to the Debrid service, verify your settings.</p>
            <Button
                intent="primary-subtle" onClick={() => {
                setEnabled(true)
            }}
            >Retry</Button>
        </div>
    </LuffyError>

    if (isLoading) return <LoadingSpinner />

    return (
        <>
            <div className="flex items-center w-full">
                <div>
                    <h2>{getServiceName(serverStatus?.debridSettings?.provider!)}</h2>
                    <p className="text-[--muted]">
                        See your debrid service torrents
                    </p>
                </div>
                <div className="flex flex-1"></div>
                <div className="flex gap-2 items-center">
                    <Button
                        intent="white-subtle"
                        leftIcon={<BiRefresh className="text-2xl" />}
                        onClick={() => {
                            refetch()
                            toast.info("Refreshed")
                        }}
                    >Refresh</Button>
                    {!!getDashboardLink(serverStatus?.debridSettings?.provider!) && (
                        <SeaLink href={getDashboardLink(serverStatus?.debridSettings?.provider!)} target="_blank">
                            <Button
                                intent="primary-subtle"
                                rightIcon={<BiLinkExternal className="text-xl" />}
                            >Dashboard</Button>
                        </SeaLink>
                    )}
                </div>
            </div>

            <div className="pb-10">
                <AppLayoutStack className={""}>

                    <div>
                        <ul className="text-[--muted] flex flex-wrap gap-4">
                            <li>Downloading: {data?.filter(t => t.status === "downloading" || t.status === "paused")?.length ?? 0}</li>
                            <li>Seeding: {data?.filter(t => t.status === "seeding")?.length ?? 0}</li>
                        </ul>
                    </div>

                    <Card className="p-0 overflow-hidden">
                        {data?.filter(Boolean)?.map(torrent => {
                            return <TorrentItem
                                key={torrent.id}
                                torrent={torrent}
                                isDownloadedLocally={downloadedTorrentIds.has(torrent.id)}
                            />
                        })}
                        {(!isLoading && !data?.length) && <LuffyError title="Nothing to see">No active torrents</LuffyError>}
                    </Card>
                </AppLayoutStack>
            </div>
        </>
    )

}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

const selectedTorrentItemAtom = atom<Debrid_TorrentItem | null>(null)
const playTorrentItemAtom = atom<Debrid_TorrentItem | null>(null)
// Torrent whose Play button was just clicked but has a local copy — shown in the
// "Play locally or stream?" choice modal.
const playChoiceTorrentAtom = atom<Debrid_TorrentItem | null>(null)
// Torrent whose Delete button was just clicked and has a local copy — shown in the
// "Delete local, remote, or both?" choice modal.
const deleteChoiceTorrentAtom = atom<Debrid_TorrentItem | null>(null)


type TorrentItemProps = {
    torrent: Debrid_TorrentItem
    isPending?: boolean
    isDownloadedLocally?: boolean
}

type DownloadProgress = {
    status: string
    itemID: string
    totalBytes: string
    totalSize: string
    speed: string
}

const TorrentItem = React.memo(function TorrentItem({ torrent, isPending, isDownloadedLocally }: TorrentItemProps) {

    const { mutate: deleteTorrent, isPending: isDeleting } = useDebridDeleteTorrent()

    const { mutate: cancelDownload, isPending: isCancelling } = useDebridCancelDownload()

    const [_, setSelectedTorrentItem] = useAtom(selectedTorrentItemAtom)
    const [__, setPlayTorrentItem] = useAtom(playTorrentItemAtom)
    const [___, setPlayChoiceTorrent] = useAtom(playChoiceTorrentAtom)
    const [____, setDeleteChoiceTorrent] = useAtom(deleteChoiceTorrentAtom)

    // Only used when the torrent is NOT downloaded locally — the multi-option
    // modal handles the downloaded case.
    const confirmDeleteTorrentProps = useConfirmationDialog({
        title: "Remove torrent",
        description: "This action cannot be undone.",
        onConfirm: () => {
            deleteTorrent({
                torrentItem: torrent,
            })
        },
    })

    const handlePlayClick = () => {
        if (isDownloadedLocally) {
            setPlayChoiceTorrent(torrent)
        } else {
            setPlayTorrentItem(torrent)
        }
    }

    const handleDeleteClick = () => {
        if (isDownloadedLocally) {
            setDeleteChoiceTorrent(torrent)
        } else {
            confirmDeleteTorrentProps.open()
        }
    }

    const [progress, setProgress] = React.useState<DownloadProgress | null>(null)

    useWebsocketMessageListener<DownloadProgress>({
        type: WSEvents.DEBRID_DOWNLOAD_PROGRESS,
        onMessage: data => {
            if (data.itemID === torrent.id) {
                if (data.status === "downloading") {
                    setProgress(data)
                } else {
                    setProgress(null)
                }
            }
        },
    })

    function handleCancelDownload() {
        cancelDownload({
            itemID: torrent.id,
        })
    }

    return (
        <div
            data-torrent-item-container className={cn(
            "hover:bg-gray-900 hover:bg-opacity-70 px-4 py-3 relative flex gap-4 group/torrent-item",
            torrent.status === "paused" && "bg-gray-900 hover:bg-gray-900",
            torrent.status === "downloading" && "bg-green-900 bg-opacity-20 hover:hover:bg-opacity-30 hover:bg-green-900",
        )}
        >
            <div className="w-full">
                <div
                    className={cn("group-hover/torrent-item:text-white break-all", {
                        "opacity-50": torrent.status === "paused",
                    })}
                >{torrent.name}</div>
                <div className="text-[--muted]">
                    <span className={cn({ "text-green-300": torrent.status === "downloading" })}>{torrent.completionPercentage}%</span>
                    {` `}
                    <BiDownArrow className="inline-block mx-2" />
                    {torrent.speed}
                    {(torrent.eta && torrent.status === "downloading") && <>
                        {` `}
                        <BiTime className="inline-block mx-2 mb-0.5" />
                        {torrent.eta}
                    </>}
                    {` - `}
                    <span className="text-[--muted]">
                        {formatDate(torrent.added, "yyyy-MM-dd HH:mm")}
                    </span>
                    {` - `}
                    <strong
                        className={cn(
                            "text-sm",
                            torrent.status === "seeding" && "text-blue-300",
                            torrent.status === "completed" && "text-green-300",
                        )}
                    >{(torrent.status === "other" || !torrent.isReady) ? "" : capitalize(torrent.status)}</strong>
                </div>
                {torrent.status !== "seeding" && torrent.status !== "completed" &&
                    <div data-torrent-item-progress-bar className="w-full h-1 mr-4 mt-2 relative z-[1] bg-gray-700 left-0 overflow-hidden rounded-xl">
                        <div
                            className={cn(
                                "h-full absolute z-[2] left-0 bg-gray-200 transition-all",
                                {
                                    "bg-green-300": torrent.status === "downloading",
                                    "bg-gray-500": torrent.status === "paused",
                                    "bg-orange-800": torrent.status === "other",
                                },
                            )}
                            style={{ width: `${String(torrent.completionPercentage)}%` }}
                        ></div>
                    </div>}
            </div>
            <div className="flex-none flex gap-2 items-center">
                {isDownloadedLocally && (
                    <Tooltip
                        trigger={
                            <span className="flex items-center">
                                <FaCheckCircle className="text-[--green] text-lg" />
                            </span>
                        }
                    >
                        Downloaded locally
                    </Tooltip>
                )}
                {(torrent.isReady && !progress) && <>
                    <Tooltip trigger={
                        <IconButton
                            icon={<IoPlayCircle className="text-lg" />}
                            size="sm"
                            intent="primary-subtle"
                            className="flex-none"
                            disabled={isDeleting || isCancelling}
                            onClick={handlePlayClick}
                        />
                    }>
                        Play
                    </Tooltip>
                    <IconButton
                        icon={<FiDownload />}
                        size="sm"
                        intent="gray-subtle"
                        className="flex-none"
                        disabled={isDeleting || isCancelling}
                        onClick={() => {
                            setSelectedTorrentItem(torrent)
                        }}
                    />
                </>}
                {(!!progress && progress.itemID === torrent.id) && <div className="flex gap-2 items-center">
                    <Tooltip
                        trigger={<p>
                            <HiFolderDownload className="text-2xl animate-pulse text-[--blue]" />
                        </p>}
                    >
                        Downloading locally
                    </Tooltip>
                    <p>
                        {progress?.totalBytes}<span className="text-[--muted]"> / {progress?.totalSize}</span>
                    </p>
                    <Tooltip
                        trigger={<p>
                            <IconButton
                                icon={<BiX className="text-xl" />}
                                intent="gray-subtle"
                                rounded
                                size="sm"
                                onClick={handleCancelDownload}
                                loading={isCancelling}
                            />
                        </p>}
                    >
                        Cancel download
                    </Tooltip>
                </div>}
                <IconButton
                    icon={<BiTrash />}
                    size="sm"
                    intent="alert-subtle"
                    className="flex-none"
                    onClick={handleDeleteClick}
                    disabled={isCancelling}
                    loading={isDeleting}
                />
            </div>
            <ConfirmationDialog {...confirmDeleteTorrentProps} />
        </div>
    )
})

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type TorrentItemModalProps = {}

function TorrentItemModal(props: TorrentItemModalProps) {
    const serverStatus = useServerStatus()

    const [selectedTorrentItem, setSelectedTorrentItem] = useAtom(selectedTorrentItemAtom)
    const { mutate: downloadTorrent, isPending: isDownloading } = useDebridDownloadTorrent()

    const [destination, setDestination] = React.useState("")

    const libraryPath = React.useMemo(() => serverStatus?.settings?.library?.libraryPath, [serverStatus])

    const libraryPathSelectionProps = useLibraryPathSelection({
        destination,
        setDestination,
    })

    React.useEffect(() => {
        if (selectedTorrentItem && libraryPath) {
            setDestination(libraryPath)
        }
    }, [selectedTorrentItem, libraryPath])

    const handleDownload = () => {
        if (!selectedTorrentItem || !destination) return
        downloadTorrent({
            torrentItem: selectedTorrentItem,
            destination: destination,
        }, {
            onSuccess: () => {
                setSelectedTorrentItem(null)
            },
        })
    }

    return (
        <Modal
            open={!!selectedTorrentItem}
            onOpenChange={() => {
                setSelectedTorrentItem(null)
            }}
            title="Download"
            contentClass="max-w-2xl"
        >
            <p className="text-center line-clamp-2 text-sm">
                {selectedTorrentItem?.name}
            </p>

            <div className="space-y-4 mt-4">
                <DirectorySelector
                    name="destination"
                    label="Destination"
                    leftIcon={<FcFolder />}
                    value={destination}
                    defaultValue={destination}
                    onSelect={setDestination}
                    shouldExist={false}
                    help="Where to save the torrent"
                    libraryPathSelectionProps={libraryPathSelectionProps}
                />

                <div className="flex justify-end">
                    <Button
                        intent="white"
                        leftIcon={<FiDownload className="text-xl" />}
                        loading={isDownloading}
                        disabled={!destination || destination.length < 2}
                        onClick={handleDownload}
                    >
                        Download
                    </Button>
                </div>
            </div>
        </Modal>
    )
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type PlayTorrentPayload = {
    torrentId: string
    fileId: string
    title: string
    clientId: string
    playLocally: boolean
}

// PlayTorrentModal fires the direct-stream play mutation whenever its atom is set.
// This is the default path for torrents that are NOT downloaded locally.
// For downloaded torrents the user goes through PlayChoiceModal which sets this atom
// (stream) or calls the mutation with playLocally=true itself.
function PlayTorrentModal() {
    const [playTorrentItem, setPlayTorrentItem] = useAtom(playTorrentItemAtom)
    const clientId = useAtomValue(clientIdAtom)

    const { mutate: playTorrent } = useServerMutation<boolean, PlayTorrentPayload>({
        endpoint: "/api/v1/debrid/torrents/play",
        method: "POST",
        mutationKey: ["debrid-play-torrent"],
    })

    React.useEffect(() => {
        if (playTorrentItem) {
            playTorrent({
                torrentId: playTorrentItem.id,
                fileId: "",
                title: playTorrentItem.name,
                clientId: clientId || "",
                playLocally: false,
            }, {
                onSuccess: () => {
                    setPlayTorrentItem(null)
                },
                onError: () => {
                    toast.error("Failed to play torrent")
                    setPlayTorrentItem(null)
                },
            })
        }
    }, [playTorrentItem])

    return null
}

// PlayChoiceModal asks the user whether to play the local copy or stream from debrid.
// Shown when the Play button is clicked on a torrent that has a DebridLocalDownload record.
function PlayChoiceModal() {
    const [playChoiceTorrent, setPlayChoiceTorrent] = useAtom(playChoiceTorrentAtom)
    const clientId = useAtomValue(clientIdAtom)

    const { mutate: playTorrent, isPending } = useServerMutation<boolean, PlayTorrentPayload>({
        endpoint: "/api/v1/debrid/torrents/play",
        method: "POST",
        mutationKey: ["debrid-play-torrent-choice"],
    })

    const handlePlay = (playLocally: boolean) => {
        if (!playChoiceTorrent) return
        playTorrent({
            torrentId: playChoiceTorrent.id,
            fileId: "",
            title: playChoiceTorrent.name,
            clientId: clientId || "",
            playLocally,
        }, {
            onSuccess: () => {
                setPlayChoiceTorrent(null)
            },
            onError: () => {
                toast.error(playLocally ? "Failed to play local file" : "Failed to stream torrent")
                setPlayChoiceTorrent(null)
            },
        })
    }

    return (
        <Modal
            open={!!playChoiceTorrent}
            onOpenChange={() => setPlayChoiceTorrent(null)}
            title="Play torrent"
            contentClass="max-w-md"
        >
            <p className="text-center line-clamp-2 text-sm text-[--muted]">
                {playChoiceTorrent?.name}
            </p>
            <p className="text-center text-sm mt-2">
                This torrent is downloaded locally. How would you like to play it?
            </p>
            <div className="flex gap-2 justify-center mt-4">
                <Button
                    intent="primary"
                    leftIcon={<IoPlayCircle className="text-lg" />}
                    loading={isPending}
                    onClick={() => handlePlay(true)}
                >
                    Play locally
                </Button>
                <Button
                    intent="white-subtle"
                    leftIcon={<IoPlayCircle className="text-lg" />}
                    loading={isPending}
                    onClick={() => handlePlay(false)}
                >
                    Stream from debrid
                </Button>
            </div>
        </Modal>
    )
}

// DeleteChoiceModal asks the user whether to delete only the local copy, only the
// remote debrid torrent, or both. Shown when the Delete button is clicked on a
// torrent that has a DebridLocalDownload record.
function DeleteChoiceModal() {
    const [deleteChoiceTorrent, setDeleteChoiceTorrent] = useAtom(deleteChoiceTorrentAtom)

    const { mutate: deleteTorrent, isPending: isDeletingRemote } = useDebridDeleteTorrent()
    const { mutate: deleteLocal, isPending: isDeletingLocal } = useDebridDeleteLocalDownload()

    const isPending = isDeletingRemote || isDeletingLocal

    const handleDeleteLocal = () => {
        if (!deleteChoiceTorrent) return
        deleteLocal({ torrentId: deleteChoiceTorrent.id }, {
            onSuccess: () => setDeleteChoiceTorrent(null),
        })
    }

    const handleDeleteRemote = () => {
        if (!deleteChoiceTorrent) return
        deleteTorrent({ torrentItem: deleteChoiceTorrent }, {
            onSuccess: () => setDeleteChoiceTorrent(null),
        })
    }

    const handleDeleteBoth = () => {
        if (!deleteChoiceTorrent) return
        const torrent = deleteChoiceTorrent
        deleteLocal({ torrentId: torrent.id }, {
            onSuccess: () => {
                deleteTorrent({ torrentItem: torrent }, {
                    onSuccess: () => setDeleteChoiceTorrent(null),
                })
            },
        })
    }

    return (
        <Modal
            open={!!deleteChoiceTorrent}
            onOpenChange={() => setDeleteChoiceTorrent(null)}
            title="Remove torrent"
            contentClass="max-w-md"
        >
            <p className="text-center line-clamp-2 text-sm text-[--muted]">
                {deleteChoiceTorrent?.name}
            </p>
            <p className="text-center text-sm mt-2">
                This torrent is downloaded locally. What do you want to remove?
            </p>
            <div className="flex flex-col gap-2 mt-4">
                <Button
                    intent="alert-subtle"
                    leftIcon={<BiTrash />}
                    loading={isPending}
                    onClick={handleDeleteLocal}
                >
                    Local files only
                </Button>
                <Button
                    intent="alert-subtle"
                    leftIcon={<BiTrash />}
                    loading={isPending}
                    onClick={handleDeleteRemote}
                >
                    Debrid torrent only
                </Button>
                <Button
                    intent="alert"
                    leftIcon={<BiTrash />}
                    loading={isPending}
                    onClick={handleDeleteBoth}
                >
                    Both local files and debrid torrent
                </Button>
            </div>
        </Modal>
    )
}
