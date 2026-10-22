import { useEffect, useRef, useState } from 'react';

export function useMobileNavigation(open: boolean, close: () => void) {
  const [narrow, setNarrow] = useState(() => window.innerWidth <= 700);
  const sidebar = useRef<HTMLElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    const resize = () => setNarrow(window.innerWidth <= 700);
    window.addEventListener('resize', resize);
    return () => window.removeEventListener('resize', resize);
  }, []);
  useEffect(() => {
    if (!narrow || !open) return;
    const root = sidebar.current;
    const buttons = () =>
      Array.from(
        root?.querySelectorAll<HTMLElement>(
          'a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),[tabindex="0"]',
        ) ?? [],
      );
    buttons()[0]?.focus();
    const key = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault();
        close();
      }
      if (event.key !== 'Tab') return;
      const items = buttons();
      const target = event.shiftKey ? items.at(-1) : items[0];
      if (
        !root?.contains(document.activeElement) ||
        document.activeElement === (event.shiftKey ? items[0] : items.at(-1))
      ) {
        event.preventDefault();
        target?.focus();
      }
    };
    document.addEventListener('keydown', key);
    return () => {
      document.removeEventListener('keydown', key);
      trigger.current?.focus();
    };
  }, [narrow, open, close]);
  return { narrow, sidebar, trigger };
}
