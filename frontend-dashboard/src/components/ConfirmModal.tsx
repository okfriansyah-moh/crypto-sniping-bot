import { useEffect, useId, useState } from "react";

type ConfirmModalProps = {
  open: boolean;
  title: string;
  description: string;
  /** User must type this exact phrase (case-sensitive) to enable confirm. */
  confirmPhrase: string;
  confirmLabel?: string;
  busy?: boolean;
  onClose: () => void;
  onConfirm: () => void | Promise<void>;
};

export function ConfirmModal({
  open,
  title,
  description,
  confirmPhrase,
  confirmLabel = "Confirm",
  busy = false,
  onClose,
  onConfirm,
}: ConfirmModalProps) {
  const titleId = useId();
  const [typed, setTyped] = useState("");

  useEffect(() => {
    if (!open) {
      setTyped("");
    }
  }, [open]);

  if (!open) {
    return null;
  }

  const phraseOk = typed === confirmPhrase;
  const canConfirm = phraseOk && !busy;

  return (
    <div className="modal-overlay" role="presentation" onClick={onClose}>
      <div
        className="modal-card"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        onClick={(e) => e.stopPropagation()}
      >
        <h2 id={titleId} className="modal-title">
          {title}
        </h2>
        <p className="modal-desc">{description}</p>
        <label className="modal-label">
          Type <code className="mono">{confirmPhrase}</code> to confirm
          <input
            className="modal-input"
            type="text"
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            autoComplete="off"
            spellCheck={false}
            disabled={busy}
          />
        </label>
        <div className="modal-actions">
          <button type="button" className="btn btn-ghost" onClick={onClose} disabled={busy}>
            Cancel
          </button>
          <button
            type="button"
            className="btn btn-danger"
            disabled={!canConfirm}
            onClick={() => void onConfirm()}
          >
            {busy ? "Working…" : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}
