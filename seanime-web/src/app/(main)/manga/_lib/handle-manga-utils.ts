import { getServerBaseUrl } from "@/api/client/server-url"
import { HibikeManga_ChapterDetails, Manga_MediaDownloadData } from "@/api/generated/types"
import { useServerHMACAuth, useServerStatus } from "@/app/(main)/_hooks/use-server-status"
import { DataGridRowSelectedEvent } from "@/components/ui/datagrid/use-datagrid-row-selection"
import { HMAC_TOKEN_REFRESH_WINDOW_MS, HMAC_TOKEN_TTL_SECONDS } from "@/lib/server/hmac-auth"
import { RowSelectionState } from "@tanstack/react-table"
import React from "react"

export function getChapterNumberFromChapter(chapter: string): number {
    const chapterNumber = chapter.match(/(\d+(\.\d+)?)/)?.[0]
    return chapterNumber ? Math.floor(parseFloat(chapterNumber)) : 0
}

export function getDecimalFromChapter(chapter: string): number {
    const chapterNumber = chapter.match(/(\d+(\.\d+)?)/)?.[0]
    return chapterNumber ? parseFloat(chapterNumber) : 0
}

export function isChapterBefore(a: string, b: string): boolean {
    // compare the decimal part of the chapter number
    return getDecimalFromChapter(a) < getDecimalFromChapter(b)
}

export function isChapterAfter(a: string, b: string): boolean {
    // compare the decimal part of the chapter number
    return getDecimalFromChapter(a) > getDecimalFromChapter(b)
}

export function useMangaReaderUtils() {
    const serverStatus = useServerStatus()
    const { getHMACTokenQueryParam, password } = useServerHMACAuth()
    const [tokenQueryParam, setTokenQueryParam] = React.useState<string>("")
    const [localPageToken, setLocalPageToken] = React.useState<string>("")

    React.useLayoutEffect(() => {
        let cancelled = false
        let expiresAt = 0
        const sign = async () => {
            if (expiresAt - Date.now() > HMAC_TOKEN_REFRESH_WINDOW_MS) return
            const signedAt = Date.now()
            const [proxyToken, localToken] = await Promise.all([
                getHMACTokenQueryParam("/api/v1/image-proxy", "&"),
                getHMACTokenQueryParam("/api/v1/manga/local-page", "?"),
            ])
            if (cancelled) return
            setTokenQueryParam(proxyToken)
            setLocalPageToken(localToken)
            // An empty token means signing failed (or no password is set), so try again next minute.
            if (proxyToken && localToken) expiresAt = signedAt + HMAC_TOKEN_TTL_SECONDS * 1000
        }
        sign()
        // Re-signs before expiry so a reader left open keeps loading pages. Checked every minute
        // rather than with one long timer, which a sleeping device can delay past expiry.
        const interval = setInterval(sign, 60_000)
        return () => {
            cancelled = true
            clearInterval(interval)
        }
    }, [password])

    const getChapterPageUrl = React.useCallback((url: string, isDownloaded: boolean | undefined, headers?: Record<string, string>) => {
        if (url.startsWith("{{manga-local-assets}}")) {
            return `${getServerBaseUrl()}/api/v1/manga/local-page/${encodeURIComponent(url)}${localPageToken}`
        }

        if (!isDownloaded) {
            if (headers && Object.keys(headers).length > 0) {
                const params = new URLSearchParams({
                    url,
                    headers: JSON.stringify(headers),
                })
                return `${getServerBaseUrl()}/api/v1/image-proxy?${params.toString()}${tokenQueryParam}`
            }

            return url
        }

        return `${getServerBaseUrl()}/manga-downloads/${url}`
    }, [tokenQueryParam, localPageToken])

    return {
        isReady: (!serverStatus?.serverHasPassword) || (!!password && !!tokenQueryParam),
        getChapterPageUrl,
    }

}

export function useMangaDownloadDataUtils(data: Manga_MediaDownloadData | undefined, loading: boolean) {

    const isChapterLocal = React.useCallback((chapter: HibikeManga_ChapterDetails | undefined) => {
        if (!chapter) return false
        return chapter.provider === "local-manga"
    }, [])

    const isChapterDownloaded = React.useCallback((chapter: HibikeManga_ChapterDetails | undefined) => {
        if (!data || !chapter) return false
        return (data?.downloaded[chapter.provider]?.findIndex(n => n.chapterId === chapter.id) ?? -1) !== -1
    }, [data])

    const isChapterQueued = React.useCallback((chapter: HibikeManga_ChapterDetails | undefined) => {
        if (!data || !chapter) return false
        return (data?.queued[chapter.provider]?.findIndex(n => n.chapterId === chapter.id) ?? -1) !== -1
    }, [data])

    const getProviderNumberOfDownloadedChapters = React.useCallback((provider: string) => {
        if (!data) return 0
        return Object.keys(data.downloaded[provider] || {}).length
    }, [data])
    return {
        isChapterDownloaded,
        isChapterQueued,
        getProviderNumberOfDownloadedChapters,
        showActionButtons: !loading,
        isChapterLocal,
    }

}

export function useMangaChapterListRowSelection() {

    const [rowSelection, setRowSelection] = React.useState<RowSelectionState>({})

    const [selectedChapters, setSelectedChapters] = React.useState<HibikeManga_ChapterDetails[]>([])

    const onSelectChange = React.useCallback((event: DataGridRowSelectedEvent<HibikeManga_ChapterDetails>) => {
        setSelectedChapters(event.data)
    }, [])
    return {
        rowSelection, setRowSelection,
        rowSelectedChapters: selectedChapters,
        onRowSelectionChange: onSelectChange,
        resetRowSelection: () => {
            setRowSelection({})
            setSelectedChapters([])
        },
        setSelectedChapters,
    }
}
