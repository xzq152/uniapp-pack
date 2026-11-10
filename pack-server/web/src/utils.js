export function fmtBytes(n) {
  if (n === null || n === undefined || n === '') return '-'
  if (n < 1024) return n + ' B'
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB'
  if (n < 1024 * 1024 * 1024) return (n / 1024 / 1024).toFixed(1) + ' MB'
  return (n / 1024 / 1024 / 1024).toFixed(2) + ' GB'
}

export function fmtTime(s) {
  if (!s) return '-'
  const d = new Date(s)
  if (Number.isNaN(d.getTime())) return String(s).slice(5, 16).replace('T', ' ')
  const p = (n) => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

export function friendlyError(raw) {
  const s = String(raw || '').trim()
  if (/找不到上传的资源|no such file|The system cannot find/.test(s)) return '上传资源不存在或已过期，请重新上传'
  if (/daemon disappeared|OOM|killed/i.test(s)) return '内存不足导致构建被中断，请调大 Docker 内存后重试'
  if (/appkey/i.test(s)) return 'appkey 未配置或与 appid 不匹配'
  if (/Gradle 编译失败|BUILD FAILED/i.test(s)) return '编译失败，点开详情看日志'
  if (/证书|keystore/i.test(s)) return '证书配置有误，请检查证书与口令'
  return s.length > 90 ? s.slice(-90) : s
}

export const PKG_RE = /^[a-zA-Z][a-zA-Z0-9_]*(\.[a-zA-Z][a-zA-Z0-9_]*)+$/
export const VNAME_RE = /^\d+(\.\d+){0,3}$/
