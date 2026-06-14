import type { ToastState } from "../hooks/useToast";

type ToastProps = {
  toast: ToastState | null;
  onDismiss: () => void;
};

export function Toast({ toast, onDismiss }: ToastProps) {
  if (!toast) {
    return null;
  }
  return (
    <div
      className={`toast toast--${toast.kind}`}
      role="status"
      aria-live="polite"
    >
      <span>{toast.message}</span>
      <button type="button" className="toast-dismiss" onClick={onDismiss} aria-label="Dismiss">
        ×
      </button>
    </div>
  );
}
