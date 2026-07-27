import { useState, useEffect, useCallback } from 'react'
import type { Toast } from '../../api/types'

export interface ToastCardProps {
  toast: Toast
  onDismiss: (id: string) => void
}

const SEVERITY_STYLES: Record<Toast['type'], { border: string; icon: string }> = {
  error:   { border: '#f44747', icon: 'close' },
  warn:    { border: '#cca700', icon: 'warning' },
  info:    { border: '#0e639c', icon: 'info' },
  success: { border: '#4ec9b0', icon: 'check' },
}

export function ToastCard({ toast, onDismiss }: ToastCardProps) {
  const [isExiting, setIsExiting] = useState(false)
  const style = SEVERITY_STYLES[toast.type]

  const dismiss = useCallback(() => {
    setIsExiting(true)
    setTimeout(() => onDismiss(toast.id), 150)
  }, [onDismiss, toast.id])

  useEffect(() => {
    const timer = setTimeout(() => {
      setIsExiting(true)
      setTimeout(() => onDismiss(toast.id), 150)
    }, 6000)
    return () => clearTimeout(timer)
  }, [toast.id, onDismiss])

  return (
    <div
      className={`w-[320px] bg-[#2d2d2d] rounded-[4px] shadow-[0_4px_12px_rgba(0,0,0,0.5)] flex flex-col pointer-events-auto border-l-[3px] overflow-hidden ${isExiting ? 'toast-exit' : 'toast-enter'}`}
      style={{ borderLeftColor: style.border }}
    >
      <div className="flex items-start p-3 gap-2">
        <span className="material-symbols-outlined text-[16px] shrink-0 mt-0.5" style={{ color: style.border }}>
          {style.icon}
        </span>
        <div className="flex-1 flex flex-col min-w-0">
          <div className="flex items-start justify-between gap-2">
            <span className="font-bold text-[13px] text-white leading-tight">{toast.title}</span>
            <span onClick={dismiss} className="material-symbols-outlined text-[14px] text-[#858585] hover:text-white cursor-pointer shrink-0">
              close
            </span>
          </div>
          {toast.detail && (
            <div className="text-[12px] text-[#cccccc] mt-1 leading-snug">{toast.detail}</div>
          )}
          {toast.shortIDs && toast.shortIDs.length > 0 && (
            <div className="flex flex-wrap gap-1 mt-2">
              {toast.shortIDs.map((id, i) => (
                <span key={i} className="bg-[#3c3c3c] text-[#cccccc] text-[11px] font-code px-1.5 py-0.5 rounded-sm">
                  {id}
                </span>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
