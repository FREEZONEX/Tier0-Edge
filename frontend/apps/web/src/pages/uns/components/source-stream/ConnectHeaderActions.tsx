import { createContext, useContext, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';

const HeaderContext = createContext<{
  target: HTMLDivElement | null;
  setTarget: (node: HTMLDivElement | null) => void;
} | null>(null);

export function ConnectHeaderProvider({ children }: { children: ReactNode }) {
  const [target, setTarget] = useState<HTMLDivElement | null>(null);
  return <HeaderContext.Provider value={{ target, setTarget }}>{children}</HeaderContext.Provider>;
}
export function ConnectHeaderSlot({ className }: { className: string }) {
  const context = useContext(HeaderContext);
  return <div className={className} ref={context?.setTarget} />;
}
export function ConnectHeaderActions({ children, embedded }: { children: ReactNode; embedded?: boolean }) {
  const context = useContext(HeaderContext);
  return context && !embedded ? (context.target ? createPortal(children, context.target) : null) : children;
}
