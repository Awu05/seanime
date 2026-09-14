import { AdminActivityStream } from "@/api/generated/types"
import { useGetAdminActivity, useTerminateProfileStream } from "@/api/hooks/admin-activity.hooks"
import { currentProfileAtom } from "@/app/(main)/_atoms/profile.atoms"
import { ConfirmationDialog, useConfirmationDialog } from "@/components/shared/confirmation-dialog"
import { LuffyError } from "@/components/shared/luffy-error"
import { PageWrapper } from "@/components/shared/page-wrapper"
import { IconButton } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { LoadingSpinner } from "@/components/ui/loading-spinner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { formatDistanceToNow } from "date-fns"
import { useAtomValue } from "jotai"
import React from "react"
import { BiX } from "react-icons/bi"

export default function Page() {
    const currentProfile = useAtomValue(currentProfileAtom)

    if (!currentProfile?.isAdmin) {
        return <LuffyError title="Admin access required">You don't have permission to view this page.</LuffyError>
    }

    return (
        <PageWrapper className="space-y-8 p-4 sm:p-8">
            <div>
                <h2>Activity</h2>
                <p className="text-[--muted]">Live connections and active torrent streams across all profiles</p>
            </div>
            <Content />
        </PageWrapper>
    )
}

function Content() {
    const { data, isLoading } = useGetAdminActivity(true)

    if (isLoading) return <LoadingSpinner />

    return (
        <div className="space-y-8">
            <section className="space-y-2">
                <h3>Connections</h3>
                <Card className="p-0 overflow-hidden">
                    <Table>
                        <TableHeader>
                            <TableRow>
                                <TableHead>Profile</TableHead>
                                <TableHead>Platform</TableHead>
                                <TableHead>Connected since</TableHead>
                            </TableRow>
                        </TableHeader>
                        <TableBody>
                            {data?.connections?.map(conn => (
                                <TableRow key={conn.profileId + conn.connectedAt}>
                                    <TableCell>{conn.profileName}</TableCell>
                                    <TableCell>{conn.platform || "web"}</TableCell>
                                    <TableCell>{conn.connectedAt ? formatDistanceToNow(new Date(conn.connectedAt), { addSuffix: true }) : "-"}</TableCell>
                                </TableRow>
                            ))}
                        </TableBody>
                    </Table>
                    {!data?.connections?.length && <LuffyError title="Nothing to see">No active connections</LuffyError>}
                </Card>
            </section>

            <section className="space-y-2">
                <h3>Active torrent streams</h3>
                <Card className="p-0 overflow-hidden">
                    <Table>
                        <TableHeader>
                            <TableRow>
                                <TableHead>Profile</TableHead>
                                <TableHead>Title</TableHead>
                                <TableHead>Progress</TableHead>
                                <TableHead>Speed</TableHead>
                                <TableHead>Seeders</TableHead>
                                <TableHead />
                            </TableRow>
                        </TableHeader>
                        <TableBody>
                            {data?.streams?.map(stream => (
                                <StreamRow key={stream.profileId} stream={stream} />
                            ))}
                        </TableBody>
                    </Table>
                    {!data?.streams?.length && <LuffyError title="Nothing to see">No active torrent streams</LuffyError>}
                </Card>
            </section>
        </div>
    )
}

function StreamRow({ stream }: { stream: AdminActivityStream }) {
    const { mutate: terminate, isPending } = useTerminateProfileStream()

    const confirmTerminateProps = useConfirmationDialog({
        title: "Terminate stream",
        description: `This will immediately stop ${stream.profileName}'s torrent stream. This action cannot be undone.`,
        actionText: "Terminate",
        actionIntent: "alert",
        onConfirm: () => {
            terminate({ profileId: stream.profileId })
        },
    })

    return (
        <TableRow>
            <TableCell>{stream.profileName}</TableCell>
            <TableCell className="max-w-xs truncate">{stream.title}</TableCell>
            <TableCell>{stream.progressPercentage.toFixed(1)}%</TableCell>
            <TableCell>{stream.downloadSpeed}</TableCell>
            <TableCell>{stream.seeders}</TableCell>
            <TableCell>
                <IconButton
                    icon={<BiX />}
                    size="sm"
                    intent="alert-subtle"
                    loading={isPending}
                    onClick={confirmTerminateProps.open}
                />
                <ConfirmationDialog {...confirmTerminateProps} />
            </TableCell>
        </TableRow>
    )
}
