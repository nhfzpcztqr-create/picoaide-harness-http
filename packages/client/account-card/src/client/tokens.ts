/**
 * Token 用量格式化（跟随界面语言）。
 *
 * Token 是整数计数，不使用货币符号或小数位；独立成模块便于在不加载
 * React/client runtime 的测试环境中直接验证格式化规则。
 */
import { activeLocaleTag } from './locales.ts'

/** Format a non-negative token count using the active UI locale. */
export function formatTokens(value: number): string {
  try {
    return new Intl.NumberFormat(activeLocaleTag(), {
      maximumFractionDigits: 0,
    }).format(Math.max(0, Math.round(value)))
  } catch {
    return String(Math.max(0, Math.round(value)))
  }
}
