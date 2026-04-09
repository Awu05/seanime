import { useGetDebridSettings, useSaveDebridSettings } from "@/api/hooks/debrid.hooks"
import { useServerStatus } from "@/app/(main)/_hooks/use-server-status"
import { AutoSelectProfileButton } from "@/app/(main)/settings/_components/autoselect-profile-form"
import { SettingsCard, SettingsPageHeader } from "@/app/(main)/settings/_components/settings-card"
import { SettingsIsDirty, SettingsSubmitButton } from "@/app/(main)/settings/_components/settings-submit-button"
import { SeaLink } from "@/components/shared/sea-link"
import { Alert } from "@/components/ui/alert"
import { defineSchema, Field, Form } from "@/components/ui/form"
import { LoadingSpinner } from "@/components/ui/loading-spinner"
import React from "react"
import { UseFormReturn } from "react-hook-form"
import { HiOutlineServerStack } from "react-icons/hi2"
import { LuCirclePlay } from "react-icons/lu"
import { toast } from "sonner"

const debridSettingsSchema = defineSchema(({ z }) => z.object({
    enabled: z.boolean().default(false),
    provider: z.string().default(""),
    apiKey: z.string().optional().default(""),
    apiUrl: z.string().optional().default(""),
    storeName: z.string().optional().default(""),
    storeApiKey: z.string().optional().default(""),
    includeDebridStreamInLibrary: z.boolean().default(false),
    streamAutoSelect: z.boolean().default(false),
    streamPreferredResolution: z.string(),
}))

type DebridSettingsProps = {
    children?: React.ReactNode
}

export function DebridSettings(props: DebridSettingsProps) {

    const {
        children,
        ...rest
    } = props

    const serverStatus = useServerStatus()
    const { data: settings, isLoading } = useGetDebridSettings()
    const { mutate, isPending } = useSaveDebridSettings()

    const formRef = React.useRef<UseFormReturn<any>>(null)

    if (isLoading) return <LoadingSpinner />

    return (
        <div className="space-y-4">

            <SettingsPageHeader
                title="Debrid Service"
                description="Configure your Debrid service integration"
                icon={HiOutlineServerStack}
            />

            <Form
                schema={debridSettingsSchema}
                mRef={formRef}
                onSubmit={data => {
                    if (settings) {
                        mutate({
                            settings: {
                                ...settings,
                                ...data,
                                provider: data.provider === "-" ? "" : data.provider,
                                storeName: data.storeName === "-" ? "" : data.storeName,
                                streamPreferredResolution: data.streamPreferredResolution === "-" ? "" : data.streamPreferredResolution,
                            },
                        },
                            {
                                onSuccess: () => {
                                    formRef.current?.reset(formRef.current.getValues())
                                    toast.success("Settings saved")
                                },
                            },
                        )
                    }
                }}
                defaultValues={{
                    enabled: settings?.enabled,
                    provider: settings?.provider || "-",
                    apiKey: settings?.apiKey,
                    apiUrl: settings?.apiUrl || "",
                    storeName: settings?.storeName || "-",
                    storeApiKey: settings?.storeApiKey || "",
                    includeDebridStreamInLibrary: settings?.includeDebridStreamInLibrary,
                    streamAutoSelect: settings?.streamAutoSelect ?? false,
                    streamPreferredResolution: settings?.streamPreferredResolution || "-",
                }}
                stackClass="space-y-4"
            >
                {(f) => (
                    <>
                        <SettingsIsDirty />
                        <SettingsCard>
                            <Field.Switch
                                side="right"
                                name="enabled"
                                label="Enable"
                            />
                            {(f.watch("enabled") && serverStatus?.settings?.autoDownloader?.enabled && !serverStatus?.settings?.autoDownloader?.useDebrid) && (
                                <Alert
                                    intent="info"
                                    title="Auto Downloader not using Debrid"
                                    description={<p>
                                        Auto Downloader is enabled but not using Debrid. Change the <SeaLink
                                            href="/auto-downloader"
                                            className="underline"
                                        >Auto Downloader settings</SeaLink> to use your Debrid service.
                                    </p>}
                                />
                            )}
                        </SettingsCard>


                        <SettingsCard>
                            <Field.Select
                                options={[
                                    { label: "None", value: "-" },
                                    { label: "TorBox", value: "torbox" },
                                    { label: "Real-Debrid", value: "realdebrid" },
                                    { label: "AllDebrid", value: "alldebrid" },
                                    { label: "StremThru", value: "stremthru" },
                                ]}
                                name="provider"
                                label="Provider"
                            />

                            {f.watch("provider") === "stremthru" && (
                                <>
                                    <Field.Text
                                        name="apiUrl"
                                        label="StremThru URL"
                                        placeholder="http://stremthru:8080"
                                        help="The base URL of your StremThru instance. Use the Docker service name if running in the same compose stack."
                                    />
                                    <Field.Text
                                        name="apiKey"
                                        label="StremThru Credentials"
                                        type="password"
                                        placeholder="username:password"
                                        help="The credentials set in STREMTHRU_AUTH (e.g. username:password)."
                                    />
                                    <Field.Select
                                        name="storeName"
                                        label="Debrid Store"
                                        help="Select the debrid store to use. Leave as Default if you only have one store configured."
                                        options={[
                                            { label: "Default", value: "-" },
                                            { label: "AllDebrid", value: "alldebrid" },
                                            { label: "Debrid-Link", value: "debridlink" },
                                            { label: "EasyDebrid", value: "easydebrid" },
                                            { label: "Offcloud", value: "offcloud" },
                                            { label: "PikPak", value: "pikpak" },
                                            { label: "Premiumize", value: "premiumize" },
                                            { label: "Real-Debrid", value: "realdebrid" },
                                            { label: "TorBox", value: "torbox" },
                                        ]}
                                    />
                                    {f.watch("storeName") !== "-" && (
                                        <Field.Text
                                            name="storeApiKey"
                                            label="Store API Key"
                                            type="password"
                                            placeholder="Your debrid service API key"
                                            help="Required for public StremThru instances. Your debrid provider's API key (e.g. TorBox API key). Leave blank for self-hosted instances where the store auth is configured server-side."
                                        />
                                    )}
                                </>
                            )}

                            {f.watch("provider") !== "stremthru" && (
                                <Field.Text
                                    name="apiKey"
                                    label="API Key"
                                    type="password"
                                />
                            )}
                        </SettingsCard>

                        <SettingsPageHeader
                            title="Debrid Streaming"
                            description="Configure how shows are streaming from your Debrid service"
                            icon={LuCirclePlay}
                        />

                        <SettingsCard title="Home Screen">
                            <Field.Switch
                                side="right"
                                name="includeDebridStreamInLibrary"
                                label="Include in anime library"
                                help="Add non-downloaded shows that are in your currently watching list to the anime library."
                            />
                        </SettingsCard>

                        <SettingsCard title="Auto-select">
                            <Field.Switch
                                side="right"
                                name="streamAutoSelect"
                                label="Enable"
                                help="Let Seanime find the best torrent automatically, based on cache and resolution."
                            />

                            {/*{f.watch("streamAutoSelect") && f.watch("provider") === "torbox" && (*/}
                            {/*    <Alert*/}
                            {/*        intent="warning-basic"*/}
                            {/*        title="Auto-select with TorBox"*/}
                            {/*        description={<p>*/}
                            {/*            Avoid using auto-select if you have a limited amount of downloads on your Debrid service.*/}
                            {/*        </p>}*/}
                            {/*    />*/}
                            {/*)}*/}

                            <Field.Select
                                name="streamPreferredResolution"
                                label="Preferred resolution"
                                help="If auto-select is enabled, Seanime will try to find torrents with this resolution."
                                options={[
                                    { label: "Highest", value: "-" },
                                    { label: "480p", value: "480" },
                                    { label: "720p", value: "720" },
                                    { label: "1080p", value: "1080" },
                                ]}
                            />

                            <div className="pt-2">
                                <AutoSelectProfileButton />
                            </div>
                        </SettingsCard>


                        <SettingsSubmitButton isPending={isPending} />
                    </>
                )}
            </Form>

        </div>
    )
}
