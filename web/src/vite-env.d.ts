/// <reference types="vite/client" />

// Allow importing CSS files
declare module '*.css' {
  const css: string
  export default css
}
