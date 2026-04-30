const BASE = '/api';

async function fetchJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const r = await fetch(`${BASE}${url}`, { headers: { 'Content-Type': 'application/json' }, ...init });
  if (!r.ok) throw new Error(await r.text().catch(() => r.statusText));
  return r.json();
}

export interface Agent {
  id: string; name: string; role: string; provider: string; model: string;
  workspace: string; system_prompt: string; max_tokens_per_day: number; budget_action: string;
}

export interface Provider {
  name: string; key: string; base: string; status: string;
}

export interface MCPServer { name: string; command: string; args: string[]; prefix: string; allowed: string[]; }
export interface DashboardData {
  status: string; agent_count: number; provider_count: number;
  gateway_host: string; gateway_port: number; workspace: string; sandboxed: boolean;
  telegram_enabled: boolean; whatsapp_enabled: boolean;
  heartbeat_enabled: boolean; mcp_enabled: boolean; ws_clients: number;
}
export type Overview = DashboardData & { uptime?: string };
export interface TaskInfo { task_id: string; objective: string; phase: string; status: string; risk_class?: string | null; }
export interface OrchStatus { running: boolean; reachable: boolean; socket: string; }

export const api = {
  dashboard: () => fetchJSON<DashboardData>('/dashboard'),
  overview: () => fetchJSON<Overview>('/dashboard'),
  health: () => fetchJSON<{status:string;version:string}>('/health'),
  agents: () => fetchJSON<Agent[]>('/agents'),
  agent: (id: string) => fetchJSON<Agent>(`/agents/${id}`),
  addAgent: (data: Partial<Agent>) => fetchJSON<Agent>('/agents/add', { method: 'POST', body: JSON.stringify(data) }),
  updateAgent: (id: string, data: Partial<Agent>) => fetchJSON<Agent>(`/agents/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  removeAgent: (id: string) => fetchJSON<{status:string}>(`/agents/remove/${id}`, { method: 'POST' }),
  providers: () => fetchJSON<Provider[]>('/providers'),
  saveProvider: (data: {name:string;key:string;base:string}) => fetchJSON<Provider[]>('/providers/save', { method: 'POST', body: JSON.stringify(data) }),
  testProvider: (name: string) => fetchJSON<{status:string;error?:string;ms:number}>(`/providers/${name}/test`, { method: 'POST' }),
  channels: () => fetchJSON<{telegram:any;whatsapp:any}>('/channels'),
  saveChannel: (name: string, data: Record<string,unknown>) => fetchJSON('/channels/'+name, { method: 'POST', body: JSON.stringify(data) }),
  config: {
    gateway: () => fetchJSON<{host:string;port:number}>('/config/gateway'),
    saveGateway: (d: {host:string;port:number}) => fetchJSON('/config/gateway', { method: 'POST', body: JSON.stringify(d) }),
    workspace: () => fetchJSON<{path:string;sandboxed:boolean}>('/config/workspace'),
    saveWorkspace: (d: {path:string;sandboxed:boolean}) => fetchJSON('/config/workspace', { method: 'POST', body: JSON.stringify(d) }),
    tools: () => fetchJSON<any>('/config/tools'),
    saveTools: (d: Record<string,unknown>) => fetchJSON('/config/tools', { method: 'POST', body: JSON.stringify(d) }),
    voice: () => fetchJSON<any>('/config/voice'),
    saveVoice: (d: Record<string,unknown>) => fetchJSON('/config/voice', { method: 'POST', body: JSON.stringify(d) }),
    heartbeat: () => fetchJSON<{enabled:boolean;interval_minutes:number}>('/config/heartbeat'),
    saveHeartbeat: (d: {enabled:boolean;interval:number}) => fetchJSON('/config/heartbeat', { method: 'POST', body: JSON.stringify(d) }),
    mcp: () => fetchJSON<{enabled:boolean;servers:MCPServer[]}>('/config/mcp'),
    addMCP: (d: MCPServer) => fetchJSON('/config/mcp', { method: 'POST', body: JSON.stringify(d) }),
    removeMCP: (name: string) => fetchJSON(`/config/mcp/${name}`, { method: 'DELETE' }),
  },
  orchestrator: () => fetchJSON<OrchStatus>('/orchestrator'),
  startOrch: () => fetchJSON<{status:string}>('/orchestrator/start', { method: 'POST' }),
  stopOrch: () => fetchJSON<{status:string}>('/orchestrator/stop', { method: 'POST' }),
  tasks: () => fetchJSON<{tasks:TaskInfo[];note:string}>('/tasks'),
  submitTask: (d: Record<string,unknown>) => fetchJSON<{status:string;task_id:string;task?:TaskInfo}>('/tasks', { method: 'POST', body: JSON.stringify(d) }),
  skills: () => fetchJSON<any>('/skills'),
  sendChat: (message: string) => fetchJSON<{status:string}>('/chat/send', { method: 'POST', body: JSON.stringify({message}) }),
};

export const sendChat = (message: string) =>
  fetchJSON<{status:string}>('/chat/send', { method: 'POST', body: JSON.stringify({message}) });
