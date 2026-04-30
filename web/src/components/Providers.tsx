import { useEffect, useState } from 'react';
import { Card, Table, Button, Badge, Modal, TextInput, Select, Group, Title } from '@mantine/core';
import { useForm } from '@mantine/form';
import { notifications } from '@mantine/notifications';
import { api, Provider } from '../api';

export default function Providers() {
  const [providers, setProviders] = useState<Provider[]>([]);
  const [modalOpen, setModalOpen] = useState(false);
  const [testing, setTesting] = useState('');
  const form = useForm({initialValues:{name:'',key:'',base:''}});

  useEffect(()=>{load()},[]);
  const load=()=>api.providers().then(setProviders).catch(()=>{});

  const save=async()=>{
    const v=form.values;
    try{await api.saveProvider(v);notifications.show({title:'Saved',message:v.name,color:'green'});setModalOpen(false);load()}
    catch(e:any){notifications.show({title:'Error',message:e.message,color:'red'})}
  };

  const test=async(name:string)=>{
    setTesting(name);
    try{const r=await api.testProvider(name);notifications.show({title:name,message:`${r.status} (${r.ms}ms)`,color:r.status==='ok'?'green':'red'})}
    catch(e:any){notifications.show({title:name,message:e.message,color:'red'})}
    setTesting('');
  };

  return <>
    <Card withBorder>
      <Group justify="space-between" mb="md">
        <Title order={4}>Providers</Title>
        <Button onClick={()=>{form.reset();setModalOpen(true)}}>Configure</Button>
      </Group>
      <Table>
        <Table.Thead><Table.Tr><Table.Th>Provider</Table.Th><Table.Th>Key</Table.Th><Table.Th>Status</Table.Th><Table.Th>Endpoint</Table.Th><Table.Th/></Table.Tr></Table.Thead>
        <Table.Tbody>
          {providers.map(p=><Table.Tr key={p.name}>
            <Table.Td fw={500}>{p.name}</Table.Td>
            <Table.Td><code style={{fontSize:11,color:'#888'}}>{p.key||'—'}</code></Table.Td>
            <Table.Td><Badge color={p.status==='configured'?'green':'gray'} variant="light">{p.status}</Badge></Table.Td>
            <Table.Td><span style={{fontSize:11,color:'#888'}}>{p.base||'default'}</span></Table.Td>
            <Table.Td><Button size="compact-sm" variant="light" loading={testing===p.name} disabled={p.status!=='configured'} onClick={()=>test(p.name)}>Test</Button></Table.Td>
          </Table.Tr>)}
        </Table.Tbody>
      </Table>
    </Card>

    <Modal opened={modalOpen} onClose={()=>setModalOpen(false)} title="Configure Provider">
      <form onSubmit={form.onSubmit(save)}>
        <Select label="Provider" data={providers.map(p=>({value:p.name,label:p.name}))} {...form.getInputProps('name')} required mb="sm"/>
        <TextInput label="API Key" {...form.getInputProps('key')} mb="sm"/>
        <TextInput label="Base URL" {...form.getInputProps('base')} placeholder="Optional" mb="lg"/>
        <Button type="submit" fullWidth>Save</Button>
      </form>
    </Modal>
  </>;
}
