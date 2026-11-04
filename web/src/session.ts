// 会话标识在存储里是 `<key_id>:<客户端标识串>`。
// 密钥维度由筛选承担,显示层只取客户端原始标识。
export function splitSessionID(id: string): { keyID: string; raw: string } {
  const at = id.indexOf(':')
  if (at <= 0) return { keyID: '', raw: id }
  return { keyID: id.slice(0, at), raw: id.slice(at + 1) }
}
