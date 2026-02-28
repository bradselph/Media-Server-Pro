import {defineConfig} from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

// https://vite.dev/config/
export default defineConfig({
    plugins: [react()],
    test: {
        environment: 'jsdom',
        globals: true,
        setupFiles: ['./src/test/setup.ts'],
        coverage: {
            provider: 'v8',
            reporter: ['text', 'lcov'],
        },
    },
    // Base path must match where Go serves the React bundle.
    // Files go to web/static/react/ (outDir below), which the Go server
    // serves at /web/static/react/ — so base must include that subdirectory.
    base: '/web/static/react/',
    build: {
        // Output built assets to web/static/react/ (inside Go's embed scope)
        outDir: path.resolve(__dirname, '../static/react'),
        emptyOutDir: true,
        rollupOptions: {
            input: {
                app: path.resolve(__dirname, 'index.html'),
            },
            output: {
                entryFileNames: 'js/[name]-[hash].js',
                chunkFileNames: 'js/[name]-[hash].js',
                assetFileNames: 'assets/[name]-[hash][extname]',
                manualChunks(id) {
                    // Split heavy vendor libraries into separate cached chunks.
                    // hls.js is listed first so it gets its own named chunk independent of
                    // the generic 'vendor' bucket. It is only ever loaded via dynamic import
                    // (useHLS.ts: await import('hls.js')), so this chunk is truly lazy.
                    if (id.includes('node_modules')) {
                        if (id.includes('hls.js')) return 'vendor-hls'
                        if (id.includes('react-dom') || id.includes('react/')) return 'vendor-react'
                        if (id.includes('react-router')) return 'vendor-router'
                        if (id.includes('@tanstack')) return 'vendor-query'
                        if (id.includes('zustand')) return 'vendor-state'
                        return 'vendor'
                    }
                },
            },
        },
    },
    resolve: {
        alias: {
            '@': path.resolve(__dirname, './src'),
            // Use the light build of hls.js to keep the lazy-loaded chunk under 500 kB.
            // The light build omits WebVTT subtitles and some rarely-used container
            // parsers but retains full ABR/adaptive streaming support.
            'hls.js': path.resolve(__dirname, 'node_modules/hls.js/dist/hls.light.min.js'),
        },
    },
    server: {
        // Proxy API requests to Go backend during development
        proxy: {
            '/api': 'http://localhost:8080',
            '/media': 'http://localhost:8080',
            '/hls': 'http://localhost:8080',
            '/thumbnail': 'http://localhost:8080',
            '/thumbnails': 'http://localhost:8080',
            '/download': 'http://localhost:8080',
            '/metrics': 'http://localhost:8080',
            '/remote': 'http://localhost:8080',
            '/health': 'http://localhost:8080',
        },
    },
})
