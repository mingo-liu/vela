import { translate, type Language } from '../../i18n'

export function formatBytes(bytes: number | null, language: Language): string {
  if (bytes === null) return translate(language, '未提供')
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB', 'TB', 'PB']
  const unit = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)) - 1, units.length - 1)
  return `${(bytes / 1024 ** (unit + 1)).toFixed(2)} ${units[unit]}`
}

export function formatDate(value: string | null, language: Language): string {
  if (!value) return translate(language, '未提供')
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? translate(language, '未提供') : date.toLocaleString(language, { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

export function subscriptionDomain(address: string, language: Language): string {
  try {
    return new URL(address).hostname || translate(language, '未知域名')
  } catch {
    return translate(language, '未知域名')
  }
}
