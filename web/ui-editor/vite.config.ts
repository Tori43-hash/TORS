import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// base: './' lets the editor be hosted under any path as plain static files.
export default defineConfig({
  base: './',
  plugins: [react()],
})
