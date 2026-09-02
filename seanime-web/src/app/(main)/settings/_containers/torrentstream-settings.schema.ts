import { defineSchema } from "@/components/ui/form/define-schema"

export const torrentstreamSchema = defineSchema(({ z }) => z.object({
    enabled: z.boolean(),
    downloadDir: z.string(),
    autoSelect: z.boolean(),
    disableIPV6: z.boolean(),
    addToLibrary: z.boolean(),
    // streamingServerPort: z.number(),
    // streamingServerHost: z.string(),
    torrentClientHost: z.string().optional().default(""),
    // Field.Number sends `undefined` when the input is cleared. The help text next to this field
    // says "Leave empty for default. Default is 43213.", so this must resolve to 43213 instead of
    // failing validation and silently blocking the save.
    torrentClientPort: z.number().optional().default(43213),
    preferredResolution: z.string(),
    includeInLibrary: z.boolean(),
    streamUrlAddress: z.string().optional().default(""),
    slowSeeding: z.boolean().optional().default(false),
    preloadNextStream: z.boolean().optional().default(false),
    disableAcceleratedStartup: z.boolean().optional().default(false),
}))
