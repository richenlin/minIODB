/** @type {import('next').NextConfig} */

const isDev = process.env.NODE_ENV === 'development'

const nextConfig = {
  ...(isDev && {
    async rewrites() {
      return [
        {
          source: '/dashboard/api/:path*',
          destination: `${process.env.MINIODB_API_URL || 'http://localhost:8081'}/dashboard/api/:path*`,
        },
      ]
    },
  }),
  images: {
    unoptimized: true,
  },
}

export default nextConfig
