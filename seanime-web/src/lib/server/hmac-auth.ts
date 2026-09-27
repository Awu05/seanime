import * as CryptoJS from "crypto-js"

// Shared by both HMAC-authenticated routes so their token lifetime can't drift apart.
export const HMAC_TOKEN_TTL_SECONDS = 24 * 60 * 60

interface TokenClaims {
    endpoint: string
    iat: number // issued at (unix timestamp)
    exp: number // expires at (unix timestamp)
}

class HMACAuth {
    private secret: string
    private ttl: number

    constructor(secret: string, ttl: number) {
        this.secret = secret
        this.ttl = ttl
    }

    // Async only to keep the existing signature for callers that already await it.
    async generateToken(endpoint: string): Promise<string> {
        return this.generateTokenSync(endpoint)
    }

    generateQueryParam(endpoint: string, symbol?: string): Promise<string> {
        return Promise.resolve(this.generateQueryParamSync(endpoint, symbol))
    }

    // For callers that build a URL synchronously (e.g. an <img src>) and can't await a token.
    generateTokenSync(endpoint: string): string {
        const now = Math.floor(Date.now() / 1000)
        const claims: TokenClaims = {
            endpoint,
            iat: now,
            exp: now + this.ttl,
        }

        const claimsJSON = JSON.stringify(claims)

        // Encode claims as base64
        const claimsB64 = btoa(claimsJSON)
            .replace(/\+/g, "-")
            .replace(/\//g, "_")
            .replace(/=/g, "")

        // Generate HMAC signature
        const signature = this.generateHMACSignatureSync(claimsB64)

        // Return token in format: claims.signature
        return `${claimsB64}.${signature}`
    }

    generateQueryParamSync(endpoint: string, symbol?: string): string {
        const token = this.generateTokenSync(endpoint)
        const sym = symbol || "?"
        return `${sym}token=${encodeURIComponent(token)}`
    }

    private generateHMACSignatureSync(data: string): string {
        const signature = CryptoJS.HmacSHA256(data, this.secret)

        const base64 = CryptoJS.enc.Base64.stringify(signature)
        return base64
            .replace(/\+/g, "-")
            .replace(/\//g, "_")
            .replace(/=/g, "")
    }
}

// HMAC auth instance using server password (for server endpoints)
export function createServerPasswordHMACAuth(password: string): HMACAuth {
    const secret = password !== "" ? password : "seanime-default-secret"
    return new HMACAuth(secret, HMAC_TOKEN_TTL_SECONDS)
}

// HMAC auth instance using Nakama password (for Nakama endpoints)
export function createNakamaHMACAuth(nakamaPassword: string): HMACAuth {
    return new HMACAuth(nakamaPassword, HMAC_TOKEN_TTL_SECONDS)
}
