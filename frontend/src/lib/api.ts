// Client for the Skiff HTTP API.

export class ApiError extends Error {
  code: string
  status: number
  constructor(message: string, code: string, status: number) {
    super(message)
    this.code = code
    this.status = status
  }
}

let onUnauthorized: (() => void) | null = null
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn
}

export async function call<T = any>(method: string, params?: any, signal?: AbortSignal): Promise<T> {
  let res: Response
  try {
    res = await fetch(`/api/rpc/${method}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Skiff': '1' },
      body: JSON.stringify(params ?? {}),
      credentials: 'same-origin',
      signal,
    })
  } catch (e: any) {
    if (e?.name === 'AbortError') throw new ApiError('Cancelled', 'cancelled', 499)
    throw new ApiError('Skiff is not reachable. Is the application still running?', 'network', 0)
  }
  let body: any = null
  try {
    body = await res.json()
  } catch {
    /* empty */
  }
  if (!res.ok) {
    if (res.status === 401 && body?.error?.code === 'auth') onUnauthorized?.()
    throw new ApiError(body?.error?.message || res.statusText, body?.error?.code || 'error', res.status)
  }
  return body?.result as T
}

export function downloadUrl(conn: string, path: string, inline = false): string {
  const q = new URLSearchParams({ conn, path })
  if (inline) q.set('inline', '1')
  return `/api/download?${q.toString()}`
}

export interface UploadProgress { loaded: number; total: number }

// uploadFile streams a browser File to conn:path.
export function uploadFile(conn: string, path: string, file: File, opts: { overwrite?: boolean; mkdirs?: boolean; onProgress?: (p: UploadProgress) => void; signal?: AbortSignal } = {}): Promise<void> {
  return new Promise((resolve, reject) => {
    const q = new URLSearchParams({ conn, path, mtime: String(file.lastModified || 0) })
    if (opts.overwrite) q.set('overwrite', '1')
    if (opts.mkdirs) q.set('mkdirs', '1')
    const xhr = new XMLHttpRequest()
    xhr.open('PUT', `/api/upload?${q.toString()}`)
    xhr.setRequestHeader('X-Skiff', '1')
    xhr.setRequestHeader('Content-Type', 'application/octet-stream')
    xhr.upload.onprogress = (e) => opts.onProgress?.({ loaded: e.loaded, total: e.total })
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) return resolve()
      let msg = xhr.statusText
      let code = 'error'
      try {
        const b = JSON.parse(xhr.responseText)
        msg = b?.error?.message || msg
        code = b?.error?.code || code
      } catch {
        /* empty */
      }
      reject(new ApiError(msg, code, xhr.status))
    }
    xhr.onerror = () => reject(new ApiError('Upload failed', 'network', 0))
    xhr.onabort = () => reject(new ApiError('Cancelled', 'cancelled', 499))
    opts.signal?.addEventListener('abort', () => xhr.abort())
    xhr.send(file)
  })
}

// ---- events -------------------------------------------------------------

export type EventHandler = (type: string, data: any) => void

export class EventStream {
  private ws: WebSocket | null = null
  private handler: EventHandler
  private retry = 0
  private closed = false
  onStatus?: (connected: boolean) => void

  constructor(handler: EventHandler) {
    this.handler = handler
    this.connect()
  }

  private connect() {
    if (this.closed) return
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const ws = new WebSocket(`${proto}://${location.host}/api/events`)
    this.ws = ws
    ws.onopen = () => {
      this.retry = 0
      this.onStatus?.(true)
    }
    ws.onmessage = (ev) => {
      try {
        const msg = JSON.parse(ev.data)
        this.handler(msg.type, msg.data)
      } catch (e) {
        console.error('bad event', e)
      }
    }
    ws.onclose = () => {
      this.onStatus?.(false)
      if (this.closed) return
      const delay = Math.min(5000, 300 * 2 ** this.retry++)
      setTimeout(() => this.connect(), delay)
    }
    ws.onerror = () => ws.close()
  }

  close() {
    this.closed = true
    this.ws?.close()
  }
}

export function termSocketUrl(id: string): string {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  return `${proto}://${location.host}/api/term/${id}`
}
