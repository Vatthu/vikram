import { useEffect, useState } from 'react';
import { Card, Table, Button, Badge, Modal, TextInput, Textarea, Group, Title, Stack, ActionIcon } from '@mantine/core';
import { useForm } from '@mantine/form';
import { notifications } from '@mantine/notifications';
import { IconPlus, IconPlayerPlay, IconPlayerStop } from '@tabler/icons-react';
import { api, TaskInfo, OrchStatus } from '../api';

export default function Tasks() {
  const [tasks, setTasks] = useState<TaskInfo[]>([]);
  const [note, setNote] = useState('');
  const [modalOpen, setModalOpen] = useState(false);
  const [orch, setOrch] = useState<OrchStatus>({running:false,reachable:false,socket:''});
  const [starting, setStarting] = useState(false);
  const form = useForm({initialValues:{objective:'',repo_path:''}});

  useEffect(()=>{loadTasks();loadOrch();},[]);
  const loadTasks=()=>api.tasks().then(d=>{setTasks(d.tasks);setNote(d.note)}).catch(()=>{});
  const loadOrch=()=>api.orchestrator().then(setOrch).catch(()=>{});

  const submit=async()=>{
    const v=form.values;
    const r=await api.submitTask(v);
    notifications.show({title:'Submitted',message:r.task_id,color:'green'});
    setModalOpen(false);form.reset();loadTasks();
  };

  const startOrch=async()=>{
    setStarting(true);
    try{await api.startOrch();notifications.show({title:'Orchestrator',message:'Started',color:'green'});setTimeout(loadOrch,2000)}
    catch(e:any){notifications.show({title:'Error',message:e.message,color:'red'})}
    setStarting(false);
  };

  const stopOrch=async()=>{
    try{await api.stopOrch();notifications.show({title:'Orchestrator',message:'Stopped',color:'orange'});setTimeout(loadOrch,1500)}
    catch(e:any){notifications.show({title:'Error',message:e.message,color:'red'})}
  };

  return <>
    <Card withBorder mb="md">
      <Group justify="space-between">
        <div>
          <Title order={4} mb={4}>Orchestrator</Title>
          <Group gap="xs">
            <Badge color={orch.reachable?'green':orch.running?'blue':'red'} variant="light" size="sm">
              {orch.reachable?'Running':orch.running?'Starting':'Stopped'}
            </Badge>
            <span style={{fontSize:11,color:'#888'}}>{orch.socket}</span>
          </Group>
        </div>
        <Group>
          <Button leftSection={<IconPlayerPlay size={16}/>} onClick={startOrch} loading={starting} color="green" variant="light" disabled={orch.reachable}>Start</Button>
          <Button leftSection={<IconPlayerStop size={16}/>} onClick={stopOrch} color="red" variant="light" disabled={!orch.running}>Stop</Button>
        </Group>
      </Group>
    </Card>

    <Card withBorder>
      <Group justify="space-between" mb="md">
        <Title order={4}>Tasks</Title>
        <Button leftSection={<IconPlus size={16}/>} onClick={()=>setModalOpen(true)}>New Task</Button>
      </Group>
      {tasks.length>0?<Table>
        <Table.Thead><Table.Tr><Table.Th>ID</Table.Th><Table.Th>Objective</Table.Th><Table.Th>Phase</Table.Th><Table.Th>Status</Table.Th><Table.Th>Risk</Table.Th></Table.Tr></Table.Thead>
        <Table.Tbody>{tasks.map(t=><Table.Tr key={t.task_id}>
          <Table.Td><code>{t.task_id}</code></Table.Td>
          <Table.Td>{t.objective?.substring(0,70)}</Table.Td>
          <Table.Td><Badge variant="light">{t.phase}</Badge></Table.Td>
          <Table.Td><Badge color={t.status==='completed'?'green':t.status==='failed'?'red':'blue'} variant="light">{t.status}</Badge></Table.Td>
          <Table.Td>{t.risk_class?<Badge color={t.risk_class==='critical'?'red':t.risk_class==='high'?'orange':'gray'} variant="light">{t.risk_class}</Badge>:'—'}</Table.Td>
        </Table.Tr>)}</Table.Tbody>
      </Table>:<div style={{textAlign:'center',padding:48,color:'#888'}}>{note}</div>}
    </Card>

    <Modal opened={modalOpen} onClose={()=>setModalOpen(false)} title="New Task">
      <form onSubmit={form.onSubmit(submit)}>
        <Textarea label="Objective" {...form.getInputProps('objective')} required rows={3} mb="sm"/>
        <TextInput label="Repository Path" {...form.getInputProps('repo_path')} placeholder="/path/to/repo" required mb="lg"/>
        <Button type="submit" fullWidth>Submit Task</Button>
      </form>
    </Modal>
  </>;
}
