export function Logo() {
  return (
    <span className="logo-mark">
      <svg width="30" height="30" viewBox="0 0 32 32" fill="none" aria-hidden="true">
        <circle cx="16" cy="16" r="10" stroke="currentColor" strokeWidth="1.6" />
        <path
          d="M16 2v4M16 26v4M2 16h4M26 16h4M6.1 6.1l2.8 2.8M23.1 23.1l2.8 2.8M6.1 25.9l2.8-2.8M23.1 8.9l2.8-2.8"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
        />
        <path
          d="M11 12h10M16 12v10"
          stroke="currentColor"
          strokeWidth="2.5"
          strokeLinecap="round"
        />
        <path d="M13 21l3 3 3-3" stroke="currentColor" strokeWidth="1.5" strokeLinejoin="round" />
      </svg>
    </span>
  );
}
