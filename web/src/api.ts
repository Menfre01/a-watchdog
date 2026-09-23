// 管理 API 客户端:同源携带会话 cookie;变更类请求统一携带 CSRF 头;
// 错误统一抛出后端返回的 message。
export interface Provider {
  id: number
  name: string
  protocol: string
  base_url: string
  api_key: string
  probe_model: string
  enabled: boolean
}

export interface Model {
  id: number
  name: string
  provider_id: number
  upstream_model: string
  enabled: boolean
}

export interface ClientKey {
  id: number
  name: string
  key_prefix: string
  enabled: boolean
  created_at: string
  key?: string
}

export interface Setting {
  key: string
  value: string
}

export interface ProbeResult {
  ok: boolean
  detail: string
  status?: number
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const resp = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: {
      'X-DS-Context-CSRF': '1',
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  if (resp.status === 204) return undefined as T
  const data = (await resp.json().catch(() => ({}))) as { error?: string }
  if (!resp.ok) {
    throw new Error(data.error ?? `请求失败(${resp.status})`)
  }
  return data as T
}

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body),
  put: <T>(path: string, body?: unknown) => request<T>('PUT', path, body),
  del: (path: string) => request<void>('DELETE', path),
}
