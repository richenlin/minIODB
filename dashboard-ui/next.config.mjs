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
  images: {
    unoptimized: true,
  },
  // In dev, proxy all /dashboard/api/* requests to the running Go backend
  // so the browser doesn't hit CORS/404 errors.
  ...(isDev && {
    async rewrites() {
      return [
        {
          source: '/dashboard/api/:path*',
          destination: `${backendOrigin}/dashboard/api/:path*`,
        },
      ]
    },
  }),
}

export default nextConfig
