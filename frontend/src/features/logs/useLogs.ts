import { useEffect, useMemo, useRef, useState } from 'react'
import * as Runtime from '../../../bindings/github.com/mingo-liu/vela/internal/desktop/runtimeservice'
import type { Page } from '../../app/types'
import { usePolling } from '../../lib/usePolling'
import { errorMessage as message } from '../../lib/errors'

type LogLevel = 'all' | 'error' | 'warning' | 'info' | 'debug' | 'other'
export const logLevels = [
  { value: 'all', label: '全部级别' },
  { value: 'error', label: '错误' },
  { value: 'warning', label: '警告' },
  { value: 'info', label: '信息日志' },
  { value: 'debug', label: '调试' },
  { value: 'other', label: '其他' },
] as const

function logLevel(line: string): Exclude<LogLevel, 'all'> {
  const level = line.match(/\blevel\s*=\s*["']?(error|fatal|warning|warn|info|debug|trace)\b/i)?.[1]
    ?? line.match(/^\s*(?:\[[^\]]+\]\s*)?\[?(error|fatal|warning|warn|info|debug|trace)\]?\s*[:\s]/i)?.[1]
  switch (level?.toLowerCase()) {
    case 'fatal': case 'error': return 'error'
    case 'warn': case 'warning': return 'warning'
    case 'info': return 'info'
    case 'trace': case 'debug': return 'debug'
    default: return 'other'
  }
}

export function useLogs(visible: boolean, page: Page) {
  const [logs, setLogs] = useState('')
  const [logsError, setLogsError] = useState('')
  const [selectedLogLevel, setSelectedLogLevel] = useState<LogLevel>('all')
  const logList = useRef<HTMLDivElement>(null)
  const followLogs = useRef(true)

  usePolling(async isCurrent => {
    try {
      const value = await Runtime.Logs()
      if (isCurrent()) { setLogs(value); setLogsError('') }
    } catch (error) {
      if (isCurrent()) setLogsError(message(error))
    }
  }, visible && page === 'logs' ? 1500 : null)

  useEffect(() => {
    if (page === 'logs' && followLogs.current && logList.current) {
      logList.current.scrollTop = logList.current.scrollHeight
    }
  }, [logs, page, selectedLogLevel])

  const logLines = useMemo(() => logs.split(/\r?\n/).filter(line => line.trim() !== ''), [logs])
  const visibleLogLines = useMemo(() => logLines.map(line => ({ line, level: logLevel(line) })).filter(entry => selectedLogLevel === 'all' || entry.level === selectedLogLevel), [logLines, selectedLogLevel])

  return {
    logsError, selectedLogLevel, logList, followLogs, logLines, visibleLogLines,
    selectLogLevel: (value: string) => { followLogs.current = true; setSelectedLogLevel(value as LogLevel) },
  }
}

export type LogsController = ReturnType<typeof useLogs>
