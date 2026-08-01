import { useMemo } from 'react'
import { ansiToHtml } from '../../utils/ansiToHtml'
import type { OutputState } from '../../hooks/useCommandOutput'

export interface TerminalOutputProps {
  outputState: OutputState
}

export function TerminalOutput({ outputState }: TerminalOutputProps) {
  const html = useMemo(
    () => outputState.status === 'ready' ? ansiToHtml(outputState.text) : '',
    [outputState],
  )

  return (
    <div className="terminal-output flex-1 flex flex-col border border-vscode-border rounded-sm overflow-hidden bg-[#0d0d0d]">
      <div className="terminal-output-toolbar bg-[#181818] border-b border-vscode-border px-3 py-1.5 flex items-center justify-between shrink-0">
        <span className="font-semibold text-vscode-text-muted flex items-center gap-2 text-[12px]">
          <span className="material-symbols-outlined text-[14px]">terminal</span> stdout
        </span>
        <div className="flex items-center gap-2 text-vscode-text-muted">
          <span className="material-symbols-outlined text-[14px] cursor-pointer hover:text-white" title="Wrap Text">wrap_text</span>
          <span className="material-symbols-outlined text-[14px] cursor-pointer hover:text-white" title="Find">search</span>
        </div>
      </div>
      {outputState.status === 'ready' ? (
        <div
          className="terminal-output-content p-3 overflow-auto font-code text-[12px] text-[#cccccc] whitespace-pre-wrap leading-relaxed select-text"
          dangerouslySetInnerHTML={{ __html: html }}
        />
      ) : (
        <div className="terminal-output-content p-3 overflow-auto font-code text-[12px] whitespace-pre-wrap leading-relaxed flex items-center gap-2 text-vscode-text-muted italic">
          {outputState.status === 'loading' && (
            <><span className="material-symbols-outlined text-[14px] animate-spin">progress_activity</span> Loading…</>
          )}
          {outputState.status === 'empty' && (
            <><span className="material-symbols-outlined text-[14px]">terminal</span> No output captured</>
          )}
          {outputState.status === 'error' && (
            <><span className="material-symbols-outlined text-[14px] text-red-400">error</span> <span className="text-red-400">Failed to load output</span></>
          )}
        </div>
      )}
    </div>
  )
}

