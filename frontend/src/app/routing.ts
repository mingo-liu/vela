export const routingModes = [
  { value: 'rule', label: '规则', description: '按配置规则分流' },
  { value: 'global', label: '全局', description: '使用 GLOBAL 策略组' },
  { value: 'direct', label: '直连', description: '全部直连' },
] as const
