import sharp from 'sharp'
import { createRequire } from 'module'
const require = createRequire(import.meta.url)
const png2icons = require('png2icons')
import { writeFileSync, mkdirSync } from 'fs'
import { resolve, dirname } from 'path'

const ROOT = resolve(dirname(new URL(import.meta.url).pathname), '..')
const OUT = resolve(ROOT, 'build')
mkdirSync(OUT, { recursive: true })

const ICON_SIZE = 1024
const PADDING = 220
const ICON_AREA = ICON_SIZE - PADDING * 2
const SCALE = ICON_AREA / 24
const STROKE = 1.8

// DiscAlbum (Lucide) on a dark gradient background, white stroke.
const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${ICON_SIZE}" height="${ICON_SIZE}" viewBox="0 0 ${ICON_SIZE} ${ICON_SIZE}">
  <defs>
    <linearGradient id="bg" x1="0%" y1="0%" x2="0%" y2="100%">
      <stop offset="0%" stop-color="#3a3a3a"/>
      <stop offset="100%" stop-color="#0a0a0a"/>
    </linearGradient>
  </defs>
  <rect width="${ICON_SIZE}" height="${ICON_SIZE}" rx="180" fill="url(#bg)"/>
  <g transform="translate(${PADDING}, ${PADDING}) scale(${SCALE})"
     stroke="#ffffff" stroke-width="${STROKE}" stroke-linecap="round" stroke-linejoin="round" fill="none">
    <rect width="18" height="18" x="3" y="3" rx="2"/>
    <circle cx="12" cy="12" r="5"/>
    <circle cx="12" cy="12" r="0.5" fill="#ffffff" stroke="none"/>
  </g>
</svg>`

async function main() {
    const pngBuffer = await sharp(Buffer.from(svg))
        .resize(ICON_SIZE, ICON_SIZE)
        .png()
        .toBuffer()
    const pngPath = resolve(OUT, 'icon.png')
    writeFileSync(pngPath, pngBuffer)
    console.log(`Created ${pngPath}`)

    const icns = png2icons.createICNS(pngBuffer, png2icons.BICUBIC2, 0)
    if (icns) {
        const icnsPath = resolve(OUT, 'icon.icns')
        writeFileSync(icnsPath, icns)
        console.log(`Created ${icnsPath}`)
    }

    console.log('Done!')
}

main().catch((err) => {
    console.error(err)
    process.exit(1)
})
