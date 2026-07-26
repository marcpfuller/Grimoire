export interface GhostIconProps {
  color?: string
  size?: number
}

export function GhostIcon({ color = 'currentColor', size = 14 }: GhostIconProps) {
  return (
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 14 14" width={size} height={size} fill={color}>
      <path d="M2 8 L2 5 C2 2.2 4 1 7 1 C10 1 12 2.2 12 5 L12 11 L10.5 9.5 L9 11 L7.5 9.5 L6 11 L4.5 9.5 L3 11 L2 8 Z" />
      <ellipse cx="5" cy="5.5" rx="1.2" ry="1.4" fill="#1e1e1e" />
      <ellipse cx="9" cy="5.5" rx="1.2" ry="1.4" fill="#1e1e1e" />
      <circle cx="5.3" cy="5.8" r="0.5" fill={color} />
      <circle cx="9.3" cy="5.8" r="0.5" fill={color} />
    </svg>
  )
}
