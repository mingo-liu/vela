import { useCallback, useEffect, useRef, type KeyboardEvent } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import type { Rule } from '../../../bindings/github.com/mingo-liu/vela/internal/mihomo/models'

export default function RuleList({ rules, label }: { rules: Rule[]; label: string }) {
  const parent = useRef<HTMLDivElement>(null)
  const getItemKey = useCallback((index: number) => rules[index].index, [rules])
  const virtualizer = useVirtualizer<HTMLDivElement, HTMLDivElement>({
    count: rules.length,
    getScrollElement: () => parent.current,
    getItemKey,
    estimateSize: () => 43,
    overscan: 6,
  })

  const remeasure = useCallback(() => {
    virtualizer.measure()
    parent.current?.querySelectorAll<HTMLDivElement>('[data-index]').forEach(virtualizer.measureElement)
  }, [virtualizer])

  // A refreshed configuration can change offscreen rows as well as mounted ones.
  useEffect(remeasure, [rules, remeasure])

  useEffect(() => {
    const element = parent.current
    if (!element) return
    let width = element.clientWidth
    const observer = new ResizeObserver(() => {
      if (element.clientWidth === width) return
      width = element.clientWidth
      // Heights measured at the old width are no longer valid after wrapping.
      remeasure()
    })
    observer.observe(element)
    return () => observer.disconnect()
  }, [remeasure])

  const navigate = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.target !== event.currentTarget || event.altKey || event.ctrlKey || event.metaKey) return
    const element = event.currentTarget
    switch (event.key) {
      case 'Home': virtualizer.scrollToIndex(0); break
      case 'End': virtualizer.scrollToIndex(rules.length - 1, { align: 'end' }); break
      case 'PageDown': virtualizer.scrollToOffset(element.scrollTop + element.clientHeight); break
      case 'PageUp': virtualizer.scrollToOffset(element.scrollTop - element.clientHeight); break
      case 'ArrowDown': virtualizer.scrollToOffset(element.scrollTop + 43); break
      case 'ArrowUp': virtualizer.scrollToOffset(element.scrollTop - 43); break
      default: return
    }
    event.preventDefault()
  }

  return <div ref={parent} className="diagnostics-list diagnostics-rule-list" role="list" aria-label={label} tabIndex={0} onKeyDown={navigate}>
    <div className="diagnostics-rule-space" role="presentation" style={{ height: virtualizer.getTotalSize() }}>
      {virtualizer.getVirtualItems().map(row => {
        const rule = rules[row.index]
        return <div
          className="diagnostics-row diagnostics-rule"
          key={row.key}
          ref={virtualizer.measureElement}
          data-index={row.index}
          data-last={row.index === rules.length - 1 || undefined}
          role="listitem"
          aria-posinset={row.index + 1}
          aria-setsize={rules.length}
          style={{ transform: `translateY(${row.start}px)` }}
        >
          <span className="diagnostics-index">{rule.index + 1}</span>
          <strong>{rule.type}</strong>
          <span title={rule.payload}>{rule.payload || '—'}</span>
          <span>{rule.proxy}</span>
        </div>
      })}
    </div>
  </div>
}
