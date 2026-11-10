import { downloadUrl } from './api'

/**
 * 多线程分段下载（局域网优先）。失败自动回退单线程。
 * @param {string} jobId
 * @param {string} name 文件名
 * @param {number} size 已知大小（可空）
 * @param {(p:number)=>void} onProgress 0~1
 */
export async function fastDownload(jobId, name, size, onProgress) {
  const url = downloadUrl(jobId, name)
  const PARTS = 4

  try {
    const head = await fetch(url, { method: 'HEAD' })
    const total = Number(head.headers.get('Content-Length') || size || 0)
    if (!head.headers.get('Accept-Ranges') || !total) {
      trigger(url, name)
      return
    }

    const chunk = Math.ceil(total / PARTS)
    const ranges = []
    for (let i = 0; i < PARTS; i++) {
      const s = i * chunk
      if (s >= total) break
      ranges.push([s, Math.min(s + chunk - 1, total - 1)])
    }

    let done = 0
    const bufs = await Promise.all(
      ranges.map(async ([s, e]) => {
        const r = await fetch(url, { headers: { Range: `bytes=${s}-${e}` } })
        const buf = new Uint8Array(await r.arrayBuffer())
        done += buf.length
        onProgress && onProgress(done / total)
        return buf
      }),
    )

    const all = new Uint8Array(total)
    let off = 0
    bufs.forEach((b) => { all.set(b, off); off += b.length })

    const blob = new Blob([all], { type: 'application/vnd.android.package-archive' })
    const obj = URL.createObjectURL(blob)
    trigger(obj, name)
    setTimeout(() => URL.revokeObjectURL(obj), 15000)
  } catch (e) {
    trigger(url, name)
  }
}

function trigger(href, name) {
  const a = document.createElement('a')
  a.href = href
  a.download = name || 'app.apk'
  document.body.appendChild(a)
  a.click()
  a.remove()
}
