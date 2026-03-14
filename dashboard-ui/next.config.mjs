/** @type {import('next').NextConfig} */
const nextConfig = {
  output: 'export',
  basePath: '/dashboard/ui',
  trailingSlash: true,
  images: {
    unoptimized: true,
  },
}

export default nextConfig
