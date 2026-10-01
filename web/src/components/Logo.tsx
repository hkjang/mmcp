export function LogoMark({ size = 32 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 64 64" aria-hidden="true">
      <defs>
        <linearGradient id="mmcp-logo-bg" x1="6" y1="4" x2="58" y2="60" gradientUnits="userSpaceOnUse">
          <stop offset="0" stopColor="#4F46E5" />
          <stop offset=".55" stopColor="#2563EB" />
          <stop offset="1" stopColor="#06B6D4" />
        </linearGradient>
      </defs>
      <rect width="64" height="64" rx="15" fill="url(#mmcp-logo-bg)" />
      <path d="M17 15h30a7 7 0 0 1 7 7v16a7 7 0 0 1-7 7H33.5l-9.2 7.4a1.2 1.2 0 0 1-1.95-.94V45H17a7 7 0 0 1-7-7V22a7 7 0 0 1 7-7Z" fill="#fff" />
      <path d="M20.5 37.5V23.8l11.5 9.4 11.5-9.4v13.7" fill="none" stroke="#2563EB" strokeWidth="3.6" strokeLinecap="round" strokeLinejoin="round" />
      <circle cx="20.5" cy="23.8" r="3.4" fill="#4F46E5" />
      <circle cx="32" cy="33.2" r="3.4" fill="#06B6D4" />
      <circle cx="43.5" cy="23.8" r="3.4" fill="#4F46E5" />
    </svg>
  )
}
