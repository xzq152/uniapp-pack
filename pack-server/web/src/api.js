const JSON_HEADERS = { 'Content-Type': 'application/json' }

async function handle(res) {
  if (res.ok) {
    const ct = res.headers.get('content-type') || ''
    return ct.includes('application/json') ? res.json() : res.text()
  }
  const text = await res.text()
  const err = new Error(text || `HTTP ${res.status}`)
  err.status = res.status
  throw err
}

export const api = {
  env: () => fetch('/api/env', { cache: 'no-store' }).then(handle),
  runtime: () => fetch('/api/runtime', { cache: 'no-store' }).then(handle),
  runtimeAction: (a) => fetch(`/api/runtime/${a}`, { method: 'POST' }).then(handle),

  login: (password) =>
    fetch('/api/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: new URLSearchParams({ password }),
    }).then(handle),
  logout: () => fetch('/api/logout', { method: 'POST' }).then(handle),

  icon: (file) => fetch('/api/icon', { method: 'PUT', body: file }).then(handle),
  splash: (file, name) => fetch('/api/splash?name=' + encodeURIComponent(name || file.name), { method: 'PUT', body: file }).then(handle),
  keystores: () => fetch('/api/keystores', { cache: 'no-store' }).then(handle),
  putKeystore: (file, meta) =>
    fetch('/api/keystore?' + new URLSearchParams(meta), { method: 'PUT', body: file }).then(handle),
  delKeystore: (id) => fetch('/api/keystores/' + id, { method: 'DELETE' }).then(handle),
  appkey: (appid) => fetch('/api/appkey?appid=' + encodeURIComponent(appid)).then(handle),

  build: (body) =>
    fetch('/api/build', { method: 'POST', headers: JSON_HEADERS, body: JSON.stringify(body) }).then(handle),
  jobs: () => fetch('/api/jobs', { cache: 'no-store' }).then(handle),
  retry: (id) => fetch(`/api/jobs/${id}/retry`, { method: 'POST' }).then(handle),
  remove: (id) => fetch(`/api/jobs/${id}`, { method: 'DELETE' }).then(handle),

  trash: () => fetch('/api/trash', { cache: 'no-store' }).then(handle),
  restore: (id) => fetch(`/api/trash/${id}/restore`, { method: 'POST' }).then(handle),
  purge: (id) => fetch(`/api/trash/${id}`, { method: 'DELETE' }).then(handle),
}

// 上传文件夹：把文件列表按「#F 相对路径\n大小\n + 内容」合成一个 Blob 一次性传
export function uploadBundle(files, rootName, onProgress) {
  return new Promise((resolve, reject) => {
    const parts = []
    for (const f of files) {
      const rel = (f.webkitRelativePath || f.name).replace(/\\/g, '/')
      if (!rel) continue
      const base = rel.split('/').pop()
      if (/^(\.DS_Store|Thumbs\.db|desktop\.ini)$/i.test(base)) continue
      parts.push(new Blob([`#F ${rel}\n${f.size}\n`]))
      parts.push(f)
    }
    if (!parts.length) return reject(new Error('文件夹是空的'))
    const blob = new Blob(parts, { type: 'application/octet-stream' })

    const xhr = new XMLHttpRequest()
    xhr.open('PUT', '/api/upload-folder?name=' + encodeURIComponent(rootName || 'folder'), true)
    xhr.upload.onprogress = (e) => {
      if (!e.lengthComputable) return
      onProgress && onProgress(e.loaded / e.total, e.loaded, e.total)
    }
    xhr.onload = () => {
      if (xhr.status === 200) resolve(JSON.parse(xhr.responseText))
      else reject(new Error(xhr.responseText || `HTTP ${xhr.status}`))
    }
    xhr.onerror = () => reject(new Error('网络错误'))
    xhr.send(blob)
  })
}

// 上传单文件（裸流，带进度）
export function uploadFile(file, onProgress) {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('PUT', '/api/upload?name=' + encodeURIComponent(file.name), true)
    xhr.upload.onprogress = (e) => {
      if (!e.lengthComputable) return
      onProgress && onProgress(e.loaded / e.total, e.loaded, e.total)
    }
    xhr.onload = () => {
      if (xhr.status === 200) resolve(JSON.parse(xhr.responseText))
      else reject(new Error(xhr.responseText || `HTTP ${xhr.status}`))
    }
    xhr.onerror = () => reject(new Error('网络错误'))
    xhr.send(file)
  })
}

export function downloadUrl(jobId, name) {
  return `/d/${jobId}/${encodeURIComponent(name)}`
}
