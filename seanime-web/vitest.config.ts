import path from "path"

// Mirrors the "@" alias declared in rsbuild.config.ts - vitest doesn't read the rsbuild config,
// so without this any test that transitively imports a "@/..." path fails to resolve.
export default {
    resolve: {
        alias: {
            "@": path.resolve(import.meta.dirname, "./src"),
        },
    },
}
