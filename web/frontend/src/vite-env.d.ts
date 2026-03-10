/// <reference types="vite/client" />

// export {} makes this a module so 'declare module "react"' below is treated
// as an augmentation (merged with the real React types) instead of an ambient
// module declaration that would replace and wipe all of React's exports.
export {}

declare module 'react' {
    // eslint-disable-next-line @typescript-eslint/no-unused-vars -- T required for interface augmentation
    interface InputHTMLAttributes<T> {
        // Non-standard Firefox attribute for vertical <input type="range"> sliders
        orient?: string
    }
}
