import { useEffect, useState } from 'react';
import { api, Overview as OV } from '../api';

export default function Overview() {
  const [data, setData] = useState<OV | null>(null);

  useEffect(() => { api.overview().then(setData).catch(() => {}); }, []);

  if (!data) return <div className="loading">Loading...</div>;

  return (
    <div>
      <div className="stats">
        <div className="stat"><div className="val">{data.agent_count}</div><div className="lbl">Team Agents</div></div>
        <div className="stat"><div className="val">{data.provider_count}</div><div className="lbl">Providers Active</div></div>
        <div className="stat"><div className="val" style={{ fontSize: 22 }}>:{data.gateway_port}</div><div className="lbl">Gateway</div></div>
        <div className="stat"><div className="val" style={{ fontSize: 16, color: 'var(--green)' }}>{data.uptime}</div><div className="lbl">System Status</div></div>
      </div>
      <div className="card">
        <div className="card-header"><h2>Quick Actions</h2></div>
        <div className="actions">
          <button className="btn btn-primary" onClick={() => document.querySelector<HTMLAnchorElement>('.sidebar a:nth-child(2)')?.dispatchEvent(new Event('click'))}>Manage Agents</button>
          <button className="btn btn-ghost" onClick={() => document.querySelector<HTMLAnchorElement>('.sidebar a:nth-child(3)')?.dispatchEvent(new Event('click'))}>Providers</button>
          <button className="btn btn-ghost" onClick={() => document.querySelector<HTMLAnchorElement>('.sidebar a:nth-child(4)')?.dispatchEvent(new Event('click'))}>Tasks</button>
        </div>
      </div>
    </div>
  );
}
