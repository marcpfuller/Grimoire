import { useRef, useState } from 'react'
import type { Command, Session } from '../../api/types'
import { GhostIcon } from '../Icons/GhostIcon'

const GHOST_ICON_SIZE = 34
const SHY_GHOST_OFFSET = 20
const DODGES_BEFORE_FURY = 4

export interface CommandHeaderProps {
  command: Command
  session: Session | null
  ghostwriterURL: string
  onCopy: () => void
}

function oplogURL(rawURL: string, oplogId: number): string | null {
  try {
    const url = new URL(rawURL)
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return null
    url.pathname = `/oplog/${oplogId}/entries`
    url.search = ''
    url.hash = ''
    return url.toString()
  } catch {
    return null
  }
}

function prefersReducedMotion(): boolean {
  return typeof window.matchMedia === 'function'
    && window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

function UnsyncedGhost() {
  const [offset, setOffset] = useState(0)
  const [isApproached, setIsApproached] = useState(false)
  const [isFurious, setIsFurious] = useState(false)
  const dodgeCount = useRef(0)
  const lastOffset = useRef(0)

  const handleMouseMove = (event: React.MouseEvent<HTMLSpanElement>) => {
    setIsApproached(true)
    if (prefersReducedMotion()) {
      setOffset(0)
      dodgeCount.current = 0
      lastOffset.current = 0
      return
    }
    if (isFurious) return

    const bounds = event.currentTarget.getBoundingClientRect()
    const mouseIsLeftOfCenter = event.clientX < bounds.left + bounds.width / 2
    const nextOffset = mouseIsLeftOfCenter ? SHY_GHOST_OFFSET : -SHY_GHOST_OFFSET

    if (nextOffset !== lastOffset.current) {
      dodgeCount.current += 1
      lastOffset.current = nextOffset
      if (dodgeCount.current >= DODGES_BEFORE_FURY) {
        setOffset(0)
        setIsFurious(true)
        return
      }
    }

    setOffset(nextOffset)
  }

  const handleMouseLeave = () => {
    setIsApproached(false)
    if (!isFurious) setOffset(0)
    lastOffset.current = 0
  }

  const handleFuryEnd = () => {
    setIsFurious(false)
    setIsApproached(false)
    setOffset(0)
    dodgeCount.current = 0
    lastOffset.current = 0
  }

  return (
    <span
      aria-disabled="true"
      title="Not yet synced to Ghostwriter"
      className="inline-flex p-1.5 text-vscode-text-muted cursor-default"
      onMouseEnter={() => setIsApproached(true)}
      onMouseMove={handleMouseMove}
      onMouseLeave={handleMouseLeave}
    >
      <span
        data-testid="unsynced-ghost"
        className={`inline-flex ${isFurious ? 'shy-ghost-fury' : ''}`}
        onAnimationEnd={handleFuryEnd}
        style={{
          transform: isFurious ? undefined : `translateX(${offset}px)`,
          filter: isApproached ? 'drop-shadow(0 0 2.5px rgba(255, 255, 255, 0.65))' : 'none',
          transition: isFurious ? 'filter 160ms ease-out' : 'transform 180ms ease-out, filter 160ms ease-out',
        }}
      >
        <GhostIcon size={GHOST_ICON_SIZE} />
      </span>
    </span>
  )
}

export function CommandHeader({ command, session, ghostwriterURL, onCopy }: CommandHeaderProps) {
  const isSuccess = command.exitCode === 0
  const exitStatusClass = isSuccess
    ? 'bg-vscode-success-bg text-vscode-success border-[#2e5230]'
    : 'bg-[#4d1f1c] text-[#ff8484] border-[#662925]'
  const exitIcon = isSuccess ? 'check_circle' : 'error'
  const ghostwriterOplogURL = command.oplogId > 0 ? oplogURL(ghostwriterURL, command.oplogId) : null

  return (
    <div className="command-header flex flex-col md:flex-row justify-between gap-4">
      <div className="command-header-info flex flex-col gap-2">
        <span className="command-header-session flex items-center gap-1 text-[12px] text-vscode-text-muted">
          <span className="material-symbols-outlined text-[14px]">terminal</span>
          Terminal – {session?.name ?? command.paneId}
        </span>
        <div className="flex items-center gap-2">
          <span className="font-semibold text-vscode-text-muted">ID: {command.id}</span>
          <div className="px-2 py-0.5 bg-[#1e1e1e] border border-vscode-border text-vscode-string font-code text-[12px] flex items-center rounded">
            {command.command}
            <span onClick={onCopy} className="material-symbols-outlined text-[14px] ml-2 text-vscode-text-muted hover:text-white cursor-pointer" title="Copy">
              content_copy
            </span>
          </div>
        </div>
        <div className="command-header-meta flex flex-wrap items-center gap-3 text-[12px] text-vscode-text-muted">
          <span className="flex items-center gap-1"><span className="material-symbols-outlined text-[14px]">schedule</span> {command.timestamp}</span>
          <span className="flex items-center gap-1"><span className="material-symbols-outlined text-[14px]">timer</span> {command.duration}</span>
          <div className={`flex items-center gap-1 px-1.5 rounded-sm border ${exitStatusClass}`}>
            <span className="material-symbols-outlined text-[14px]">{exitIcon}</span> Exit: {command.exitCode}
          </div>
        </div>
      </div>

      <div className="command-header-actions flex flex-wrap items-center gap-2">
        {command.syncedGW && ghostwriterOplogURL ? (
          <a
            href={ghostwriterOplogURL}
            target="_blank"
            rel="noreferrer"
            aria-label="Open Ghostwriter oplog"
            title="Open Ghostwriter oplog"
            className="p-1.5 text-vscode-success transition-colors hover:text-white"
          >
            <GhostIcon size={GHOST_ICON_SIZE} />
          </a>
        ) : !command.syncedGW ? (
          <UnsyncedGhost key={command.id} />
        ) : (
          <span
            aria-disabled="true"
            title="Ghostwriter oplog link unavailable"
            className="p-1.5 text-vscode-success"
          >
            <GhostIcon size={GHOST_ICON_SIZE} />
          </span>
        )}
      </div>
    </div>
  )
}
