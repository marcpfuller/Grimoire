import type { Config } from 'tailwindcss'

export default {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        'vscode-bg': '#1e1e1e',
        'vscode-sidebar': '#252526',
        'vscode-activity': '#333333',
        'vscode-border': '#3c3c3c',
        'vscode-accent': '#0e639c',
        'vscode-status': '#007acc',
        'vscode-text': '#cccccc',
        'vscode-text-muted': '#858585',
        'vscode-string': '#ce9178',
        'vscode-hover': '#2a2d2e',
        'vscode-active': '#37373d',
        'vscode-tab-bg': '#2d2d2d',
        'vscode-tab-active': '#1e1e1e',
        'vscode-success': '#7cc37a',
        'vscode-success-bg': '#233f24',
        'vscode-highlight': '#ffcc00',
      },
      spacing: {
        'activity-w': '48px',
        'sidebar-w': '250px',
        'status-h': '22px',
        'tab-h': '35px',
      },
      fontFamily: {
        code: ['Cascadia Code', 'JetBrains Mono', 'monospace'],
        display: ['Cinzel Decorative', 'Georgia', 'serif'],
        ui: ['Inter', 'sans-serif'],
      },
    },
  },
  plugins: [],
} satisfies Config
