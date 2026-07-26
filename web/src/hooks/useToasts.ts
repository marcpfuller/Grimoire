import { useState, useCallback } from 'react'
import type { Toast } from '../api/types'

type ToastInput = Omit<Toast, 'id'>

export function useToasts(): {
  toasts: Toast[]
  addToast: (toast: ToastInput) => void
  removeToast: (id: string) => void
} {
  const [toasts, setToasts] = useState<Toast[]>([
    { id: crypto.randomUUID(), type: 'error', title: 'Sync failed', detail: 'Could not fetch history from pane-a1b2', shortIDs: ['pane-a1b2'] },
    { id: crypto.randomUUID(), type: 'info', title: 'Daemon connected', detail: 'Listening on 127.0.0.1:8080' },
  ])

  const addToast = useCallback((toast: ToastInput) => {
    setToasts(prev => [{ ...toast, id: crypto.randomUUID() }, ...prev].slice(0, 3))
  }, [])

  const removeToast = useCallback((id: string) => {
    setToasts(prev => prev.filter(t => t.id !== id))
  }, [])

  return { toasts, addToast, removeToast }
}
