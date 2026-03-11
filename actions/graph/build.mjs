// build.mjs — esbuild script for pre-compiling the graph component.
// Bundles cytoscape from node_modules while externalizing shared deps
// (React, MUI, Refine) to window.__LAUNCHR_SHARED__ globals.
//
// Usage (inside container): node build.mjs

import * as esbuild from 'esbuild'
import { writeFileSync } from 'fs'

// Mirror the shared globals from launchr/web server/components.go sharedGlobalsPlugin().
const exactExternals = {
  'react':                 '__LAUNCHR_SHARED__.React',
  'react-dom':             '__LAUNCHR_SHARED__.ReactDOM',
  'react-dom/client':      '__LAUNCHR_SHARED__.ReactDOM',
  'react/jsx-runtime':     '__LAUNCHR_SHARED__.ReactJSXRuntime',
  'react/jsx-dev-runtime': '__LAUNCHR_SHARED__.ReactJSXRuntime',
  '@mui/material':         '__LAUNCHR_SHARED__.MUI',
  '@mui/icons-material':   '__LAUNCHR_SHARED__.MUIIcons',
  '@mui/x-data-grid':      '__LAUNCHR_SHARED__.MUIDataGrid',
  '@refinedev/core':       '__LAUNCHR_SHARED__.RefineCore',
}

const prefixExternals = {
  '@mui/icons-material/': '__LAUNCHR_SHARED__.MUIIcons',
  '@mui/material/':       '__LAUNCHR_SHARED__.MUI',
}

/** @type {esbuild.Plugin} */
const sharedGlobalsPlugin = {
  name: 'shared-globals',
  setup(build) {
    // Exact matches.
    for (const [path, global] of Object.entries(exactExternals)) {
      const escaped = path.replace(/[.*+?^${}()|[\]\\\/]/g, '\\$&')
      build.onResolve({ filter: new RegExp(`^${escaped}$`) }, () => ({
        path,
        namespace: 'shared-global',
      }))
    }

    build.onLoad({ filter: /.*/, namespace: 'shared-global' }, (args) => {
      const global = exactExternals[args.path]
      if (!global) return undefined
      return {
        contents: `module.exports = ${global};`,
        loader: 'js',
      }
    })

    // Prefix matches for subpath imports.
    for (const [prefix, global] of Object.entries(prefixExternals)) {
      const escaped = prefix.replace(/[.*+?^${}()|[\]\\\/]/g, '\\$&')
      build.onResolve({ filter: new RegExp(`^${escaped}`) }, (args) => ({
        path: args.path,
        namespace: 'shared-global-subpath',
      }))
    }

    build.onLoad({ filter: /.*/, namespace: 'shared-global-subpath' }, (args) => {
      for (const [prefix, global] of Object.entries(prefixExternals)) {
        if (args.path.startsWith(prefix)) {
          const sub = args.path.slice(prefix.length).replace(/\//g, '.')
          return {
            contents: `module.exports = ${global}.${sub};`,
            loader: 'js',
          }
        }
      }
      return undefined
    })
  },
}

const result = await esbuild.build({
  entryPoints: ['component.tsx'],
  bundle: true,
  write: false,
  format: 'iife',
  globalName: '__exports',
  target: 'es2020',
  jsx: 'automatic',
  jsxImportSource: 'react',
  minify: true,
  plugins: [sharedGlobalsPlugin],
})

if (result.errors.length > 0) {
  console.error('Build errors:', result.errors)
  process.exit(1)
}

const bundled = result.outputFiles[0].text

// Prepend pre-compiled marker and append module.exports assignment.
const output = `/* @pre-compiled */\n${bundled}\nmodule.exports = __exports.default;\n`

writeFileSync('component.js', output)
console.log('Built component.js successfully')
