import { useEffect, useState } from 'react';
import { Card, Tabs, TextInput, NumberInput, Switch, Button, Select, Title, Group, Table, ActionIcon } from '@mantine/core';
import { useForm } from '@mantine/form';
import { notifications } from '@mantine/notifications';
import { IconTrash } from '@tabler/icons-react';
import { api } from '../api';

function GWPanel() {
  const form = useForm({initialValues:{host:'',port:0}});
  useEffect(()=>{api.config.gateway().then(d=>form.setValues(d)).catch(()=>{})},[]);
  return <Card withBorder>
    <form onSubmit={form.onSubmit(async v=>{await api.config.saveGateway(v);notifications.show({title:'Saved',message:'Gateway',color:'green'})})}>
      <TextInput label="Host" {...form.getInputProps('host')} mb="sm"/>
      <NumberInput label="Port" {...form.getInputProps('port')} mb="lg"/>
      <Button type="submit">Save</Button>
    </form>
  </Card>;
}

function WSPanel() {
  const form = useForm({initialValues:{path:'',sandboxed:true}});
  useEffect(()=>{api.config.workspace().then(d=>form.setValues(d)).catch(()=>{})},[]);
  return <Card withBorder>
    <form onSubmit={form.onSubmit(async v=>{await api.config.saveWorkspace(v);notifications.show({title:'Saved',message:'Workspace',color:'green'})})}>
      <TextInput label="Path" {...form.getInputProps('path')} mb="sm"/>
      <Switch label="Sandboxed" {...form.getInputProps('sandboxed',{type:'checkbox'})} mb="lg"/>
      <Button type="submit">Save</Button>
    </form>
  </Card>;
}

function ToolsPanel() {
  const form = useForm({initialValues:{duckduckgo:true,brave:false,perplexity:false,cron_timeout:5}});
  useEffect(()=>{api.config.tools().then(d=>form.setValues({duckduckgo:d.web.duckduckgo.enabled,brave:d.web.brave.enabled,perplexity:d.web.perplexity.enabled,cron_timeout:d.cron.exec_timeout_minutes})).catch(()=>{})},[]);
  return <Card withBorder>
    <form onSubmit={form.onSubmit(async v=>{await api.config.saveTools({duckduckgo_enabled:v.duckduckgo,brave_enabled:v.brave,perplexity_enabled:v.perplexity,cron_timeout:v.cron_timeout});notifications.show({title:'Saved',message:'Tools',color:'green'})})}>
      <Switch label="DuckDuckGo Search" {...form.getInputProps('duckduckgo',{type:'checkbox'})} mb="sm"/>
      <Switch label="Brave Search" {...form.getInputProps('brave',{type:'checkbox'})} mb="sm"/>
      <Switch label="Perplexity" {...form.getInputProps('perplexity',{type:'checkbox'})} mb="sm"/>
      <NumberInput label="Cron Timeout (min)" {...form.getInputProps('cron_timeout')} mb="lg"/>
      <Button type="submit">Save</Button>
    </form>
  </Card>;
}

function MCPPanel() {
  const [data,setData]=useState<any>(null);
  const form=useForm({initialValues:{name:'',command:'npx',args:'',prefix:''}});
  useEffect(()=>{api.config.mcp().then(setData).catch(()=>{})},[]);
  const add=async()=>{
    const v=form.values;
    await api.config.addMCP({...v,args:v.args.split(' ').filter(Boolean),allowed:[]} as any);
    const d=await api.config.mcp();setData(d);notifications.show({title:'Added',message:v.name,color:'green'});
  };
  const remove=async(n:string)=>{await api.config.removeMCP(n);const d=await api.config.mcp();setData(d);notifications.show({title:'Removed',message:n})};
  return <>
    <Card withBorder mb="md">
      <Title order={4} mb="md">MCP Servers</Title>
      <Table>
        <Table.Thead><Table.Tr><Table.Th>Name</Table.Th><Table.Th>Command</Table.Th><Table.Th>Prefix</Table.Th><Table.Th/></Table.Tr></Table.Thead>
        <Table.Tbody>{(data?.servers||[]).map((s:any)=><Table.Tr key={s.name}><Table.Td>{s.name}</Table.Td><Table.Td><code>{s.command} {s.args?.join(' ')}</code></Table.Td><Table.Td>{s.prefix}</Table.Td><Table.Td><ActionIcon color="red" variant="light" onClick={()=>remove(s.name)}><IconTrash size={14}/></ActionIcon></Table.Td></Table.Tr>)}</Table.Tbody>
      </Table>
    </Card>
    <Card withBorder>
      <Title order={4} mb="md">Add MCP Server</Title>
      <form onSubmit={form.onSubmit(add)}>
        <TextInput label="Name" {...form.getInputProps('name')} required mb="sm" placeholder="playwright"/>
        <TextInput label="Command" {...form.getInputProps('command')} required mb="sm" placeholder="npx"/>
        <TextInput label="Args" {...form.getInputProps('args')} mb="sm" placeholder="-y @anthropic/mcp-playwright"/>
        <TextInput label="Tool Prefix" {...form.getInputProps('prefix')} required mb="lg" placeholder="mcp_pw_"/>
        <Button type="submit">Add Server</Button>
      </form>
    </Card>
  </>;
}

function VoicePanel() {
  const form=useForm({initialValues:{enabled:false,mode:'push-to-talk',tts_provider:'auto'}});
  useEffect(()=>{api.config.voice().then(d=>form.setValues(d)).catch(()=>{})},[]);
  return <Card withBorder>
    <form onSubmit={form.onSubmit(async v=>{await api.config.saveVoice(v);notifications.show({title:'Saved',message:'Voice',color:'green'})})}>
      <Switch label="Enabled" {...form.getInputProps('enabled',{type:'checkbox'})} mb="sm"/>
      <Select label="Mode" data={[{value:'push-to-talk',label:'Push to Talk'},{value:'wake-word',label:'Wake Word'},{value:'always-on',label:'Always On'}]} {...form.getInputProps('mode')} mb="sm"/>
      <Select label="TTS Provider" data={[{value:'openai',label:'OpenAI'},{value:'edge',label:'Edge'},{value:'auto',label:'Auto'}]} {...form.getInputProps('tts_provider')} mb="lg"/>
      <Button type="submit">Save</Button>
    </form>
  </Card>;
}

function HeartbeatPanel() {
  const form=useForm({initialValues:{enabled:true,interval:30}});
  useEffect(()=>{api.config.heartbeat().then(d=>form.setValues({enabled:d.enabled,interval:d.interval_minutes})).catch(()=>{})},[]);
  return <Card withBorder>
    <form onSubmit={form.onSubmit(async v=>{await api.config.saveHeartbeat(v);notifications.show({title:'Saved',message:'Heartbeat',color:'green'})})}>
      <Switch label="Enabled" {...form.getInputProps('enabled',{type:'checkbox'})} mb="sm"/>
      <NumberInput label="Interval (minutes)" {...form.getInputProps('interval')} mb="lg"/>
      <Button type="submit">Save</Button>
    </form>
  </Card>;
}

export default function Configuration() {
  return (
    <Tabs defaultValue="gateway">
      <Tabs.List mb="md">
        <Tabs.Tab value="gateway">Gateway</Tabs.Tab>
        <Tabs.Tab value="workspace">Workspace</Tabs.Tab>
        <Tabs.Tab value="tools">Tools</Tabs.Tab>
        <Tabs.Tab value="mcp">MCP</Tabs.Tab>
        <Tabs.Tab value="voice">Voice</Tabs.Tab>
        <Tabs.Tab value="heartbeat">Heartbeat</Tabs.Tab>
      </Tabs.List>
      <Tabs.Panel value="gateway"><GWPanel/></Tabs.Panel>
      <Tabs.Panel value="workspace"><WSPanel/></Tabs.Panel>
      <Tabs.Panel value="tools"><ToolsPanel/></Tabs.Panel>
      <Tabs.Panel value="mcp"><MCPPanel/></Tabs.Panel>
      <Tabs.Panel value="voice"><VoicePanel/></Tabs.Panel>
      <Tabs.Panel value="heartbeat"><HeartbeatPanel/></Tabs.Panel>
    </Tabs>
  );
}
