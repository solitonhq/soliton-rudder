import { writeFileSync } from 'node:fs'
import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    {
      // Vite empties dist/ on every build, but the Go side embeds `frontend/dist` and needs it to be non-empty
      // on a fresh clone, so a tracked placeholder is restored after each build.
      name: 'keep-dist-placeholder',
      closeBundle() {
        writeFileSync('dist/.gitkeep', '')
      },
    },
  ],
  test: { environment: 'jsdom', include: ['src/**/*.test.{ts,tsx}'] },
})
