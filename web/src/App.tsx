import { useState, useEffect, useCallback } from 'react';
import { MantineProvider, AppShell, NavLink, Badge, createTheme } from '@mantine/core';
import { Notifications } from '@mantine/notifications';
import { IconDashboard, IconUsers, IconApi, IconMessage, IconSettings, IconChecklist, IconMessageChatbot } from '@tabler/icons-react';
import Chat from './components/Chat';
import Dashboard from './components/Dashboard';
import Agents from './components/Agents';
import Providers from './components/Providers';
import Channels from './components/Channels';
import Configuration from './components/Configuration';
import Tasks from './components/Tasks';
import '@mantine/core/styles.css';
import '@mantine/notifications/styles.css';

const darkTheme = createTheme({
  primaryColor: 'blue',
  fontFamily: 'Lexend, system-ui, sans-serif',
});

type Tab = 'chat'|'dashboard'|'agents'|'providers'|'channels'|'config'|'tasks';
const tabs:{id:Tab;label:string;icon:any}[] = [
  {id:'chat',label:'Chat',icon:IconMessageChatbot},
  {id:'dashboard',label:'Dashboard',icon:IconDashboard},
  {id:'agents',label:'Agents',icon:IconUsers},
  {id:'providers',label:'Providers',icon:IconApi},
  {id:'channels',label:'Channels',icon:IconMessage},
  {id:'config',label:'Configuration',icon:IconSettings},
  {id:'tasks',label:'Tasks',icon:IconChecklist},
];

export default function App() {
  const [tab, setTab] = useState<Tab>('chat');
  const [wsLive, setWsLive] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const bump = useCallback(() => setRefresh(k => k + 1), []);

  useEffect(() => {
    let ws:WebSocket; let t:ReturnType<typeof setTimeout>;
    (function c(){ws=new WebSocket(`ws://${location.host}/ws`);ws.onopen=()=>setWsLive(true);ws.onclose=()=>{setWsLive(false);t=setTimeout(c,2000)};ws.onmessage=(e)=>{
  bump();
  // Forward all WS messages so Chat component can receive them.
  window.dispatchEvent(new MessageEvent('message',{data:e.data}));
}})();
    return ()=>{ws?.close();clearTimeout(t)};
  }, [bump]);

  return (
    <MantineProvider theme={darkTheme} defaultColorScheme="dark">
      <Notifications position="top-right" />
      <AppShell navbar={{width:240,breakpoint:0}} padding="xl">
        <AppShell.Navbar p="sm">
          <AppShell.Section p="md">
            <div style={{fontSize:20,fontWeight:700,letterSpacing:2,fontFamily:"'Share Tech Mono',monospace"}}>
              <span style={{color:'var(--mantine-color-blue-4)'}}>Le</span>Vik
            </div>
            <div style={{fontSize:10,color:'var(--mantine-color-gray-6)',letterSpacing:3,textTransform:'uppercase',marginTop:4,fontFamily:"'Share Tech Mono',monospace"}}>Control Panel</div>
          </AppShell.Section>
          <AppShell.Section grow>
            {tabs.map(t => <NavLink key={t.id} label={t.label} leftSection={<t.icon size={18}/>} active={tab===t.id} onClick={()=>setTab(t.id)} variant="filled" />)}
          </AppShell.Section>
          <AppShell.Section p="md">
            <Badge color={wsLive?'green':'red'} size="sm" variant="dot">{wsLive?'live':'offline'}</Badge>
          </AppShell.Section>
        </AppShell.Navbar>
        <AppShell.Main>
          {tab==='chat'&&<Chat wsLive={wsLive}/>}
          {tab==='dashboard'&&<Dashboard key={refresh}/>}
          {tab==='agents'&&<Agents key={refresh}/>}
          {tab==='providers'&&<Providers key={refresh}/>}
          {tab==='channels'&&<Channels key={refresh}/>}
          {tab==='config'&&<Configuration key={refresh}/>}
          {tab==='tasks'&&<Tasks key={refresh}/>}
        </AppShell.Main>
      </AppShell>
    </MantineProvider>
  );
}
