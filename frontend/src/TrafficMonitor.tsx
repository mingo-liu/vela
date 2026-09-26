import { useEffect, useState } from 'react'
import { ArrowDown, ArrowUp } from '@phosphor-icons/react'
import * as Runtime from '../bindings/github.com/mingo-liu/vela/internal/desktop/runtimeservice'
import { translate, type Language } from './i18n'

type Sample = { upload: number; download: number }

const sampleCount = 32
const zero: Sample = { upload: 0, download: 0 }
const initialHistory = Array.from({ length: sampleCount }, () => zero)

function formatRate(bytesPerSecond: number): { value: string; unit: string } {
  const units = ['B/s', 'KB/s', 'MB/s', 'GB/s', 'TB/s']
  let value = bytesPerSecond
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit++
  }
  return { value: value.toFixed(2), unit: units[unit] }
}

function chartPath(history: Sample[], key: keyof Sample, max: number): string {
  const x = (index: number) => index * 320 / (sampleCount - 1)
  const y = (sample: Sample) => 86 - sample[key] / max * 72
  return history.reduce((path, sample, index) => {
    if (index === 0) return `M 0 ${y(sample)}`
    const previous = history[index - 1]
    const middle = (x(index - 1) + x(index)) / 2
    return `${path} C ${middle} ${y(previous)}, ${middle} ${y(sample)}, ${x(index)} ${y(sample)}`
  }, '')
}

export default function TrafficMonitor({ language }: { language: Language }) {
  const [history, setHistory] = useState<Sample[]>(initialHistory)

  useEffect(() => {
    let active = true
    let pending = false
    let previous: { upload: number; download: number; interface: string; time: number } | null = null
    const refresh = async () => {
      if (pending) return
      pending = true
      let sample = zero
      try {
        const totals = await Runtime.TrafficTotals()
        const now = Date.now()
        if (previous && previous.interface === totals.interface) {
          const seconds = Math.max((now - previous.time) / 1000, 0.001)
          sample = {
            upload: Math.max(0, (totals.uploadTotal - previous.upload) / seconds),
            download: Math.max(0, (totals.downloadTotal - previous.download) / seconds),
          }
        }
        previous = { upload: totals.uploadTotal, download: totals.downloadTotal, interface: totals.interface, time: now }
      } catch {
        previous = null
      } finally {
        pending = false
      }
      if (active) setHistory(current => [...current.slice(1), sample])
    }

    void refresh()
    const timer = window.setInterval(() => void refresh(), 1000)
    return () => { active = false; window.clearInterval(timer) }
  }, [])

  const latest = history[history.length - 1]
  const upload = formatRate(latest.upload)
  const download = formatRate(latest.download)
  const max = Math.max(1024, ...history.flatMap(sample => [sample.upload, sample.download]))

  return <section className="sidebar-traffic" aria-label={translate(language, '系统网络速度')}>
    <svg className="traffic-chart" viewBox="0 0 320 94" preserveAspectRatio="none" aria-hidden="true">
      <path className="traffic-guide" d="M 0 14 H 320 M 0 50 H 320 M 0 86 H 320" />
      <path className="traffic-upload-line" d={chartPath(history, 'upload', max)} />
      <path className="traffic-download-line" d={chartPath(history, 'download', max)} />
    </svg>
    <div className="traffic-rate traffic-rate-upload" aria-label={`${translate(language, '上传速度')} ${upload.value} ${upload.unit}`}>
      <ArrowUp size={19} aria-hidden="true" />
      <span className="traffic-value">{upload.value}</span>
      <span className="traffic-unit">{upload.unit}</span>
    </div>
    <div className="traffic-rate traffic-rate-download" aria-label={`${translate(language, '下载速度')} ${download.value} ${download.unit}`}>
      <ArrowDown size={19} aria-hidden="true" />
      <span className="traffic-value">{download.value}</span>
      <span className="traffic-unit">{download.unit}</span>
    </div>
  </section>
}
