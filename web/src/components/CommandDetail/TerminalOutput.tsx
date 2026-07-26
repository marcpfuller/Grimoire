export interface TerminalOutputProps {
  output: string
}

export function TerminalOutput({ output }: TerminalOutputProps) {
  return (
    <div className="flex-1 flex flex-col border border-vscode-border rounded-sm overflow-hidden bg-[#0d0d0d]">
      <div className="bg-[#181818] border-b border-vscode-border px-3 py-1.5 flex items-center justify-between shrink-0">
        <span className="font-semibold text-vscode-text-muted flex items-center gap-2 text-[12px]">
          <span className="material-symbols-outlined text-[14px]">terminal</span> stdout
        </span>
        <div className="flex items-center gap-2 text-vscode-text-muted">
          <span className="material-symbols-outlined text-[14px] cursor-pointer hover:text-white" title="Wrap Text">wrap_text</span>
          <span className="material-symbols-outlined text-[14px] cursor-pointer hover:text-white" title="Find">search</span>
        </div>
      </div>
      <div className="p-3 overflow-auto font-code text-[12px] text-[#cccccc] whitespace-pre-wrap leading-relaxed select-text">
        {output}
      </div>
    </div>
  )
}
