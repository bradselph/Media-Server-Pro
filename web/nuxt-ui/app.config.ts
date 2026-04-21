export default defineAppConfig({
    ui: {
        colors: {
            primary: 'indigo',
            neutral: 'zinc',
        },
    },
    /* Build-time brand defaults. A deploy-time override is also supported via
       a <script> tag that sets window.APP_CONFIG before the app boots (see
       composables/useBrandConfig). This enables a single build to serve
       multiple brands by swapping only the HTML shell. */
    brand: {
        name: 'Media Server Pro',
        tagline: 'Your Library',
        /* When gradient is '' (empty), the brand logo falls back to the
           accent-hue-derived gradient defined inline in default.vue. */
        gradient: '',
    },
})
