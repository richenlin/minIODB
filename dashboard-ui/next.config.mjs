/** @type {import('next').NextConfig} */

// MINIODB_API_URL: URL of the Go backend (e.g. http://localhost:8081).
// Required when Next.js runs as a standalone server separate from the Go process.
const backendUrl = process.env.MINIODB_API_URL || 'http://localhost:8081'

if (!process.env.MINIODB_API_URL && process.env.NODE_ENV === 'production') {
  console.warn(
    '[next.config] MINIODB_API_URL is not set. ' +
    'API proxy will fall back to http://localhost:8081. ' +
    'Set MINIODB_API_URL to point to the Go backend in production.'
  )
}

const nextConfig = {
  async rewrites() {
    return [
      {
        source: '/dashboard/api/:path*',
        destination: `${backendUrl}/dashboard/api/:path*`,
      },
    ]
  },
  images: {
    unoptimized: true,
  },
}

export default nextConfig
