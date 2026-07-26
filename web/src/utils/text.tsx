import type { ReactNode } from 'react'

/**
 * Renders `text` with `query` occurrences wrapped in a highlight span.
 * Testable via @testing-library/react render.
 */
export function highlightText(text: string, query: string): ReactNode {
  if (!query) return text
  const parts = text.split(new RegExp(`(${query})`, 'gi'))
  return (
    <>
      {parts.map((part, index) =>
        part.toLowerCase() === query.toLowerCase() ? (
          <span key={index} className="text-vscode-highlight">
            {part}
          </span>
        ) : (
          part
        )
      )}
    </>
  )
}

/**
 * Pure split — testable with plain `expect()`, no render required.
 */
export function splitHighlight(text: string, query: string): string[] {
  if (!query) return [text]
  return text.split(new RegExp(`(${query})`, 'gi'))
}
