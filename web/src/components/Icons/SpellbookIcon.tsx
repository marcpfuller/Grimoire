export interface SpellbookIconProps {
  color?: string
  size?: number
}

export function SpellbookIcon({ color = 'currentColor', size = 14 }: SpellbookIconProps) {
  return (
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 14 14" width={size} height={size} fill="none" stroke={color} strokeWidth="1">
      <path d="M1 2.5 C1 2 1.5 1.5 2 1.5 L6.5 1.5 L6.5 11.5 L2 11.5 C1.5 11.5 1 11 1 10.5 Z" />
      <path d="M7.5 1.5 L12 1.5 C12.5 1.5 13 2 13 2.5 L13 10.5 C13 11 12.5 11.5 12 11.5 L7.5 11.5 Z" />
      <line x1="7" y1="1.5" x2="7" y2="11.5" />
      <line x1="2.5" y1="4" x2="6" y2="4" strokeWidth="0.75" />
      <line x1="2.5" y1="5.5" x2="6" y2="5.5" strokeWidth="0.75" />
      <line x1="2.5" y1="7" x2="5" y2="7" strokeWidth="0.75" />
      <line x1="8" y1="4" x2="11.5" y2="4" strokeWidth="0.75" />
      <line x1="8" y1="5.5" x2="11.5" y2="5.5" strokeWidth="0.75" />
      <line x1="8" y1="7" x2="10.5" y2="7" strokeWidth="0.75" />
      <circle cx="7" cy="1.5" r="0.75" fill={color} stroke="none" />
      <circle cx="7" cy="11.5" r="0.75" fill={color} stroke="none" />
    </svg>
  )
}
