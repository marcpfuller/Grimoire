import type { Toast } from '../../api/types'
import { ToastCard } from './ToastCard'

export interface ToastContainerProps {
  toasts: Toast[]
  removeToast: (id: string) => void
}

export function ToastContainer({ toasts, removeToast }: ToastContainerProps) {
  return (
    <div className="fixed bottom-[30px] right-[16px] z-[1000] flex flex-col-reverse gap-2 pointer-events-none">
      {toasts.map(toast => (
        <ToastCard key={toast.id} toast={toast} onDismiss={removeToast} />
      ))}
    </div>
  )
}
