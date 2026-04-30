import { useEffect, useState } from 'react';
import { Card, Table, Button, Badge, Modal, TextInput, Select, NumberInput, Group, Title, ActionIcon } from '@mantine/core';
import { useForm } from '@mantine/form';
import { notifications } from '@mantine/notifications';
import { IconPlus, IconTrash } from '@tabler/icons-react';
import { api, Agent } from '../api';

const roleColors:Record<string,string>={lead:'blue',engineer:'grape',reviewer:'orange',runner:'green',qa:'cyan'};

export default function Agents() {
  const [agents, setAgents] = useState<Agent[]>([]);
  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<Agent|null>(null);
  const form = useForm({initialValues:{id:'',name:'',role:'',provider:'',model:'',max_tokens_per_day:0,budget_action:'notify'}});

  useEffect(()=>{load()},[]);
  const load=()=>api.agents().then(setAgents).catch(()=>{});
  const openAdd=()=>{setEditing(null);form.reset();setModalOpen(true)};
  const openEdit=(a:Agent)=>{setEditing(a);form.setValues(a);setModalOpen(true)};

  const save=async()=>{
    const v=form.values;
    try{
      if(editing) await api.updateAgent(editing.id,v);
      else await api.addAgent(v);
      notifications.show({title:editing?'Updated':'Added',message:`Agent ${v.id}`,color:'green'});
      setModalOpen(false);load();
    }catch(e:any){notifications.show({title:'Error',message:e.message,color:'red'})}
  };

  const remove=async(id:string)=>{await api.removeAgent(id);notifications.show({title:'Removed',message:id,color:'orange'});load()};

  return <>
    <Card withBorder>
      <Group justify="space-between" mb="md">
        <Title order={4}>Team Agents</Title>
        <Button leftSection={<IconPlus size={16}/>} onClick={openAdd}>Add Agent</Button>
      </Group>
      <Table>
        <Table.Thead><Table.Tr><Table.Th>ID</Table.Th><Table.Th>Role</Table.Th><Table.Th>Provider</Table.Th><Table.Th>Model</Table.Th><Table.Th>Budget</Table.Th><Table.Th/></Table.Tr></Table.Thead>
        <Table.Tbody>
          {agents.map(a=><Table.Tr key={a.id}>
            <Table.Td><code>{a.id}</code></Table.Td>
            <Table.Td><Badge color={roleColors[a.role]||'gray'} variant="light">{a.role?.toUpperCase()}</Badge></Table.Td>
            <Table.Td>{a.provider||'default'}</Table.Td>
            <Table.Td><code style={{fontSize:12}}>{a.model||'default'}</code></Table.Td>
            <Table.Td>{a.max_tokens_per_day>0?Math.round(a.max_tokens_per_day/1000)+'K':'∞'}</Table.Td>
            <Table.Td>
              <Group gap="xs">
                <Button size="compact-sm" variant="light" onClick={()=>openEdit(a)}>Edit</Button>
                <ActionIcon color="red" variant="light" onClick={()=>remove(a.id)}><IconTrash size={14}/></ActionIcon>
              </Group>
            </Table.Td>
          </Table.Tr>)}
        </Table.Tbody>
      </Table>
    </Card>

    <Modal opened={modalOpen} onClose={()=>setModalOpen(false)} title={editing?`Edit: ${editing.id}`:'Add Agent'} size="lg">
      <form onSubmit={form.onSubmit(save)}>
        <TextInput label="Agent ID" {...form.getInputProps('id')} disabled={!!editing} required mb="sm"/>
        <TextInput label="Name" {...form.getInputProps('name')} mb="sm"/>
        <Select label="Role" data={[
          {value:'lead',label:'Lead Engineer'},{value:'engineer',label:'Engineer'},
          {value:'reviewer',label:'Code Reviewer'},{value:'runner',label:'Test Runner'},{value:'qa',label:'QA Engineer'}
        ]} {...form.getInputProps('role')} required mb="sm"/>
        <TextInput label="Provider" {...form.getInputProps('provider')} mb="sm"/>
        <TextInput label="Model" {...form.getInputProps('model')} mb="sm"/>
        <NumberInput label="Max Tokens/Day (0=unlimited)" {...form.getInputProps('max_tokens_per_day')} min={0} mb="sm"/>
        <Select label="Budget Action" data={[
          {value:'notify',label:'Notify on limit'},{value:'stop',label:'Hard stop on limit'}
        ]} {...form.getInputProps('budget_action')} mb="lg"/>
        <Button type="submit" fullWidth>Save</Button>
      </form>
    </Modal>
  </>;
}
