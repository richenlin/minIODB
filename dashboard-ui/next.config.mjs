/** @type {import('next').NextConfig} */

const isDev = process.env.NODE_ENV === 'development'

// Go dashboard server address for local dev proxy.
// Override with MINIODB_DASHBOARD_BACKEND=http://host:port when needed.
const backendOrigin = process.env.MINIODB_DASHBOARD_BACKEND ?? 'http://localhost:9090'

const nextConfig = {
  // Static export is only used for production builds (Go embeds the output/).
  // In dev mode we need a real Next.js server to support rewrites/proxy.
  ...(!isDev && { output: 'export' }),
  basePath: '/dashboard/ui',
  trailingSlash: true,
  // trailingSlash controls the static-export file layout (page → page/index.html).
  // skipTrailingSlashRedirect prevents Next.js from auto-redirecting
  // /dashboard/api/v1/tables → /dashboard/api/v1/tables/ in dev mode,
  // which would cause 404s on the Go backend (Gin routes have no trailing slash).
  skipTrailingSlashRedirect: true,
  images: {
    unoptimized: true,
  },
  // In dev, proxy all /dashboard/api/* requests to the running Go backend.
  // basePath: false is required — without it Next.js prepends the basePath
  // (/dashboard/ui) to the source, making it never match the absolute
  // /dashboard/api/... URLs that client.ts sends.
  ...(isDev && {
    async rewrites() {
      return [
        {
          basePath: false,
          source: '/dashboard/api/:path*',
          destination: `${backendOrigin}/dashboard/api/:path*`,
        },
      ]
    },
  }),
}

export default nextConfig
