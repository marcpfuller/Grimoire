import type { ApiClient, DaemonHealth, AppConfig, Session, Tag } from './types'
import type { MetadataResponse } from './generated'

const MOCK_SESSIONS: Session[] = [
  {
    id: 'pane-a1b2',
    name: 'pane-a1b2',
    commands: [
      { id: 'abc0001', command: 'nmap -sC -sV 10.0.0.5', timestamp: '2h ago', duration: '45s', exitCode: 0, paneId: 'pane-a1b2', syncedGW: true },
      { id: 'abc0002', command: 'ffuf -w wordlist.txt -u http://10.0.0.5/FUZZ', timestamp: '1h ago', duration: '5m', exitCode: 1, paneId: 'pane-a1b2', syncedGW: false },
      { id: 'abc0003', command: 'cat /etc/passwd', timestamp: '30m ago', duration: '1s', exitCode: 0, paneId: 'pane-a1b2', syncedGW: false },
    ],
  },
  {
    id: 'pane-c3d4',
    name: 'pane-c3d4',
    commands: [
      { id: 'def0001', command: 'ssh user@192.168.1.10', timestamp: '1d ago', duration: '2h', exitCode: 0, paneId: 'pane-c3d4', syncedGW: true },
      { id: 'def0002', command: 'ls -la /var/www/html', timestamp: '1d ago', duration: '1s', exitCode: 0, paneId: 'pane-c3d4', syncedGW: false },
    ],
  },
]

const MOCK_TAGS: Tag[] = [
  // commandId is the raw database primary key (distinct from shortId, as it is
  // in the real backend) so mock data exercises the same id mismatch the real
  // API produces instead of masking it.
  { id: 'tag1', commandId: '1', shortId: 'abc0001', label: 'Reconnaissance', commandText: 'nmap -sC -sV 10.0.0.5', paneId: 'pane-a1b2', createdAt: '2h ago' },
  { id: 'tag2', commandId: '2', shortId: 'abc0002', label: 'Web', commandText: 'ffuf -w wordlist.txt -u http://10.0.0.5/FUZZ', paneId: 'pane-a1b2', createdAt: '1h ago' },
  { id: 'tag3', commandId: '3', shortId: 'abc0003', label: 'Credential Access', commandText: 'cat /etc/passwd', paneId: 'pane-a1b2', createdAt: '30m ago' },
]

const MOCK_OUTPUT = `Starting Nmap 7.94 ( https://nmap.org ) at 2024-05-20 14:32 UTC
Nmap scan report for 10.0.0.5
Host is up (0.0012s latency).
Not shown: 997 closed tcp ports (reset)
PORT     STATE SERVICE VERSION
22/tcp   open  ssh     OpenSSH 8.9p1 Ubuntu 3ubuntu0.6 (Ubuntu Linux; protocol 2.0)
| ssh-hostkey:
|   256 8f:e4:6b:39:13:52:90:3d:e0:62:35:b0:7a:5e:3c:2a (ECDSA)
|_  256 5e:b0:4c:3a:ed:44:26:9e:20:9b:a4:23:7e:36:58:1c (ED25519)
80/tcp   open  http    nginx 1.18.0 (Ubuntu)
|_http-server-header: nginx/1.18.0 (Ubuntu)
|_http-title: Welcome to nginx!
3306/tcp open  mysql   MySQL 8.0.36-0ubuntu0.22.04.1
| mysql-info:
|   Protocol: 10
|   Version: 8.0.36-0ubuntu0.22.04.1
|   Thread ID: 8
|   Capabilities flags: 65535`

const MOCK_CONFIG: AppConfig = {
  ghostwriter: { url: 'https://ghostwriter.example.com', tokens: ['gw_sk-1234567890abcdef'] },
  sync: { interval_seconds: 30 },
  cast: { dir: '/tmp/grimoire/casts' },
  log: { level: 'info', dir: '.', max_mb: 50, max_backups: 5 },
}

const delay = (ms: number) => new Promise<void>(resolve => setTimeout(resolve, ms))

const MOCK_METADATA: Record<string, MetadataResponse> = {
  abc0001: {
    operator: 'jsmith',
    tool: 'nmap',
    userContext: 'CORP\\jsmith',
    sourceIp: '10.10.1.5',
    destinationIp: '10.10.1.20',
    description: 'Initial port scan of target',
    comments: '',
    tags: ['att&ck:T1046'],
  },
  abc0002: {
    operator: 'jsmith',
    tool: 'ffuf',
    userContext: '',
    sourceIp: '10.10.1.5',
    destinationIp: '10.0.0.5',
    description: '',
    comments: '',
    tags: [],
  },
}

const DEFAULT_METADATA: MetadataResponse = {
  operator: '',
  tool: '',
  userContext: '',
  sourceIp: '',
  destinationIp: '',
  description: '',
  comments: '',
  tags: [],
}

export const MockAPI: ApiClient = {
  fetchHealth: () =>
    delay(100).then(() => ({ status: 'Connected', errors: 0, warnings: 0, version: 'v0.1.0-dev' } satisfies DaemonHealth)),
  fetchConfig: () => delay(100).then(() => MOCK_CONFIG),
  updateConfig: config => delay(300).then(() => config),
  exportCast: _ids => delay(300).then(() => ({ success: true, path: '/tmp/grimoire/casts/grimoire-export.cast' })),
  fetchSessions: () => delay(100).then(() => MOCK_SESSIONS),
  fetchTags: () => delay(100).then(() => MOCK_TAGS),
  fetchOutput: _commandId => delay(100).then(() => MOCK_OUTPUT),
  fetchCommandMetadata: commandId =>
    delay(100).then(() => MOCK_METADATA[commandId] ?? { ...DEFAULT_METADATA, tool: commandId.split('').shift() ?? '' }),
  patchCommandMetadata: (commandId, req) =>
    delay(300).then(() => {
      const current = MOCK_METADATA[commandId] ?? { ...DEFAULT_METADATA }
      MOCK_METADATA[commandId] = {
        ...current,
        ...(req.operator !== undefined ? { operator: req.operator } : {}),
        ...(req.tool !== undefined ? { tool: req.tool } : {}),
        ...(req.userContext !== undefined ? { userContext: req.userContext } : {}),
        ...(req.sourceIp !== undefined ? { sourceIp: req.sourceIp } : {}),
        ...(req.destinationIp !== undefined ? { destinationIp: req.destinationIp } : {}),
        ...(req.description !== undefined ? { description: req.description } : {}),
        ...(req.comments !== undefined ? { comments: req.comments } : {}),
        tags: [
          ...current.tags.filter(t => !(req.removeTags ?? []).includes(t)),
          ...(req.addTags ?? []),
        ],
      }
    }),
  syncCommand: shortId =>
    delay(300).then(() => {
      for (const s of MOCK_SESSIONS) {
        const cmd = s.commands.find(c => c.id === shortId)
        if (cmd) { cmd.syncedGW = true; return }
      }
      throw new Error(`command not found: ${shortId}`)
    }),
}
