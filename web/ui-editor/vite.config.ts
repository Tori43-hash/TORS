import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// base: './' lets the builder be hosted under any path as plain static files.
// The module index lives in the repository root (registry/index.json).
export default defineConfig({
  base: './',
  plugins: [react()],
  server: { fs: { allow: ['../..'] } },
  build: { chunkSizeWarningLimit: 800 },
})
