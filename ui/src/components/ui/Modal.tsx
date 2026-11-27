"use client";

import { useEffect, useId, useRef } from "react";
import { X } from "lucide-react";

// Modal on the native <dialog>: the browser traps focus, makes the page behind
// inert, handles Escape and returns focus to the trigger on close.
export function Modal({
  title,
  onClose,
  children,
}: {
  title: string;
  onClose: () => void;
  children: React.ReactNode;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();

  useEffect(() => {
    const dialog = ref.current;
    if (dialog && !dialog.open) dialog.showModal();
    // A dialog doesn't stop the page behind it from scrolling.
    const previous = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = previous;
      if (dialog?.open) dialog.close();
    };
  }, []);

  return (
    <dialog
      ref={ref}
      aria-labelledby={titleId}
      onCancel={(event) => {
        // Route Escape through our own close so state stays the source of truth.
        event.preventDefault();
        onClose();
      }}
      onClick={(event) => {
        if (event.target === ref.current) onClose(); // click on the backdrop
      }}
      className="m-auto w-[700px] max-w-[calc(100vw-2rem)] rounded-[10px] border border-app-border bg-app-surface p-0 text-fg shadow-2xl backdrop:bg-slate-900/60"
    >
      <div className="flex max-h-[90vh] flex-col">
        <header className="flex items-center justify-between px-6 py-5">
          <h2 id={titleId} className="text-lg font-semibold text-fg">{title}</h2>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close"
            className="grid h-8 w-8 cursor-pointer place-items-center rounded-btn text-fg-muted hover:text-fg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500"
          >
            <X aria-hidden="true" className="h-[18px] w-[18px]" />
          </button>
        </header>
        {children}
      </div>
    </dialog>
  );
}

export function ModalBody({ children }: { children: React.ReactNode }) {
  return <div className="min-h-0 flex-1 overflow-y-auto px-6 pb-5">{children}</div>;
}

export function ModalFooter({ children }: { children: React.ReactNode }) {
  return <footer className="flex items-center justify-between gap-3 border-t border-app-border px-6 py-4">{children}</footer>;
}