import { useEffect, useRef, useState, type ReactNode } from 'react';

export function TableScroll({
  children,
  className = '',
}: {
  children: ReactNode;
  className?: string;
}) {
  const viewport = useRef<HTMLDivElement>(null);
  const [overflow, setOverflow] = useState(false);
  useEffect(() => {
    const root = viewport.current;
    if (!root) return;
    const update = () => setOverflow(root.scrollWidth > root.clientWidth + 1);
    update();
    const observer = new ResizeObserver(update);
    observer.observe(root);
    if (root.firstElementChild) observer.observe(root.firstElementChild);
    return () => observer.disconnect();
  }, []);
  return (
    <>
      {overflow && <p className="table-scroll-hint">Scroll horizontally to see more columns.</p>}
      <div
        ref={viewport}
        className={`table-scroll ${className}`}
        tabIndex={overflow ? 0 : undefined}
        role={overflow ? 'group' : undefined}
        aria-label={overflow ? 'Scrollable table' : undefined}
      >
        {children}
      </div>
    </>
  );
}
