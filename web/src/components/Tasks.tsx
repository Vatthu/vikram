import { useCallback, useEffect, useRef, useState } from 'react';
import { Badge, Button, Card, Divider, Group, Modal, Paper, Stack, Table, Text, TextInput, Textarea, Title } from '@mantine/core';
import { useForm } from '@mantine/form';
import { notifications } from '@mantine/notifications';
import { IconPlus, IconPlayerPlay, IconPlayerStop } from '@tabler/icons-react';
import { ActionTransition, api, ApprovalDecisionKind, OrchStatus, ReviewAction, TaskInfo, TaskReviewDetail } from '../api';

function operatorState(task: TaskInfo): { label: string; color: string } {
  if (task.status === 'awaiting_approval' || task.requires_founder_review) {
    return { label: 'awaiting_approval', color: 'yellow' };
  }
  if (task.merge_readiness === 'blocked') {
    return { label: task.follow_up_required ? 'retryable' : 'blocked', color: 'red' };
  }
  if (task.merge_readiness === 'ready') {
    return { label: 'merge_ready', color: 'green' };
  }
  if (task.follow_up_required) {
    return { label: 'retryable', color: 'orange' };
  }
  return { label: task.status, color: task.status === 'failed' ? 'red' : 'blue' };
}

function actionLabel(action: ReviewAction): string {
  if (action === 'edit_and_approve') return 'Edit & Approve';
  if (action === 'retry_change') return 'Retry Change';
  return action.replace('_', ' ').replace(/\b\w/g, m => m.toUpperCase());
}

function actionColor(action: ReviewAction): string {
  if (action === 'reject') return 'red';
  if (action === 'clarify' || action === 'retry_change') return 'orange';
  if (action === 'edit_and_approve') return 'blue';
  return 'green';
}

function isDecisionAction(action: ReviewAction): action is ApprovalDecisionKind {
  return action === 'approve' || action === 'reject' || action === 'edit_and_approve' || action === 'clarify';
}

export default function Tasks() {
  const [tasks, setTasks] = useState<TaskInfo[]>([]);
  const [note, setNote] = useState('');
  const [selectedTaskId, setSelectedTaskId] = useState('');
  const [review, setReview] = useState<TaskReviewDetail | null>(null);
  const [reviewLoading, setReviewLoading] = useState(false);
  const [decisionLoading, setDecisionLoading] = useState(false);
  const [decisionComment, setDecisionComment] = useState('');
  const [modalOpen, setModalOpen] = useState(false);
  const [orch, setOrch] = useState<OrchStatus>({running:false,reachable:false,socket:''});
  const [starting, setStarting] = useState(false);
  const form = useForm({initialValues:{objective:'',repoPath:''}});

  const orchReloadTimeouts = useRef<number[]>([]);
  const loadTasks = useCallback(
    () => api.tasks().then(d => { setTasks(d.tasks); setNote(d.note); }).catch(() => {}),
    []
  );
  const loadOrch = useCallback(
    () => api.orchestrator().then(setOrch).catch(() => {}),
    []
  );
  useEffect(() => { loadTasks(); loadOrch(); }, [loadTasks, loadOrch]);
  useEffect(() => {
    return () => {
      orchReloadTimeouts.current.forEach((timeoutId) => window.clearTimeout(timeoutId));
      orchReloadTimeouts.current = [];
    };
  }, []);

  const loadReview = async (taskId: string) => {
    setSelectedTaskId(taskId);
    setReviewLoading(true);
    try {
      const detail = await api.taskReview(taskId);
      setReview(detail);
      setDecisionComment('');
    } catch (e: any) {
      notifications.show({title:'Error',message:e.message,color:'red'});
      setReview(null);
    } finally {
      setReviewLoading(false);
    }
  };

  const submit=async()=>{
    try {
      const r=await api.submitTask({ objective: form.values.objective, repo_path: form.values.repoPath });
      notifications.show({title:'Submitted',message:r.task_id,color:'green'});
      setModalOpen(false);
      form.reset();
      await loadTasks();
    } catch(e:any) {
      notifications.show({title:'Error',message:e.message,color:'red'});
    }
  };

  const startOrch=async()=>{
    setStarting(true);
    try{
      await api.startOrch();
      notifications.show({title:'Orchestrator',message:'Started',color:'green'});
      const timeoutId = window.setTimeout(loadOrch,2000);
      orchReloadTimeouts.current.push(timeoutId);
    }
    catch(e:any){notifications.show({title:'Error',message:e.message,color:'red'})}
    finally{setStarting(false);}
  };

  const stopOrch=async()=>{
    try{
      await api.stopOrch();
      notifications.show({title:'Orchestrator',message:'Stopped',color:'orange'});
      const timeoutId = window.setTimeout(loadOrch,1500);
      orchReloadTimeouts.current.push(timeoutId);
    }
    catch(e:any){notifications.show({title:'Error',message:e.message,color:'red'})}
  };

  const submitDecision = async (decision: ApprovalDecisionKind) => {
    if (!review) return;
    if ((decision === 'clarify' || decision === 'edit_and_approve') && !decisionComment.trim()) {
      notifications.show({title:'Founder comment required',message:'Clarification and edit requests need explicit instructions.',color:'orange'});
      return;
    }
    setDecisionLoading(true);
    try {
      const next = await api.resumeTask(review.task.task_id, {
        task_id: review.task.task_id,
        decision,
        comment: decisionComment.trim(),
        proposed_edits: {},
      });
      notifications.show({title:'Decision saved',message:next.phase,color:'green'});
      setDecisionComment('');
      await loadTasks();
      await loadReview(next.task_id);
    } catch (e: any) {
      notifications.show({title:'Error',message:e.message,color:'red'});
    } finally {
      setDecisionLoading(false);
    }
  };

  const transitions = review?.action_transitions ?? [];
  const decisionTransitions = transitions.filter((item): item is ActionTransition & { action: ApprovalDecisionKind } => (
    item.enabled !== false && item.state === 'awaiting_approval' && isDecisionAction(item.action)
  ));
  const signalTransitions = transitions.filter(item => item.state !== 'awaiting_approval' || !isDecisionAction(item.action));

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
        <Table.Thead><Table.Tr><Table.Th>ID</Table.Th><Table.Th>Objective</Table.Th><Table.Th>Phase</Table.Th><Table.Th>Status</Table.Th><Table.Th>Operator State</Table.Th><Table.Th>Risk</Table.Th></Table.Tr></Table.Thead>
        <Table.Tbody>{tasks.map(t=><Table.Tr key={t.task_id} style={{cursor:'pointer', background:selectedTaskId===t.task_id?'rgba(255,255,255,0.04)':'transparent'}} onClick={()=>loadReview(t.task_id)}>
          <Table.Td><code>{t.task_id}</code></Table.Td>
          <Table.Td>{t.objective?.substring(0,70)}</Table.Td>
          <Table.Td><Badge variant="light">{t.phase}</Badge></Table.Td>
          <Table.Td><Badge color={t.status==='completed'?'green':t.status==='failed'?'red':'blue'} variant="light">{t.status}</Badge></Table.Td>
          <Table.Td><Badge color={operatorState(t).color} variant="light">{operatorState(t).label}</Badge></Table.Td>
          <Table.Td>{t.risk_class?<Badge color={t.risk_class==='critical'?'red':t.risk_class==='high'?'orange':'gray'} variant="light">{t.risk_class}</Badge>:'-'}</Table.Td>
        </Table.Tr>)}</Table.Tbody>
      </Table>:<div style={{textAlign:'center',padding:48,color:'#888'}}>{note}</div>}

      <Divider my="xl" />

      <Stack gap="md">
        <Group justify="space-between">
          <Title order={4}>Founder Command Center</Title>
          {review && <Badge color={operatorState(review.task).color} variant="light">{operatorState(review.task).label}</Badge>}
        </Group>
        {!review && <Text c="dimmed" size="sm">Select a task to inspect review evidence and its explicit operator state transitions.</Text>}
        {review && (
          <>
            <Paper p="md" withBorder>
              <Group justify="space-between" mb="xs" align="flex-start">
                <div>
                  <Title order={5}>{review.task.objective}</Title>
                  <Text size="xs" c="dimmed">{review.task.task_id} · {review.task.phase}</Text>
                </div>
                <Badge variant="light">{review.task.status}</Badge>
              </Group>
              <Text size="sm">{review.task.summary || 'No task summary recorded yet.'}</Text>
            </Paper>

            <Paper p="md" withBorder>
              <Title order={5} mb="xs">Available Founder Decisions</Title>
              <Textarea
                label="Founder comment"
                description="Required for clarification and edit requests. Stored as part of the task evidence."
                value={decisionComment}
                onChange={(event) => setDecisionComment(event.currentTarget.value)}
                minRows={3}
                mb="md"
              />
              {decisionTransitions.length > 0 ? (
                <Stack gap="xs">
                  {decisionTransitions.map((transition) => (
                    <Group key={transition.action} justify="space-between" wrap="nowrap">
                      <div>
                        <Text size="sm" fw={600}>{actionLabel(transition.action)}</Text>
                        <Text size="xs" c="dimmed">{transition.summary}</Text>
                        <Text size="xs" c="dimmed">Target: {transition.target_phase} / {transition.target_status}</Text>
                      </div>
                      <Button
                        variant={transition.action === 'approve' ? 'filled' : 'light'}
                        color={actionColor(transition.action)}
                        loading={decisionLoading}
                        onClick={() => submitDecision(transition.action)}
                      >
                        {actionLabel(transition.action)}
                      </Button>
                    </Group>
                  ))}
                </Stack>
              ) : (
                <Text c="dimmed" size="sm">No resume decision is currently available for this task.</Text>
              )}
            </Paper>

            <Paper p="md" withBorder>
              <Title order={5} mb="xs">Operator State Machine</Title>
              {reviewLoading ? <Text size="sm">Loading review...</Text> : (
                <Stack gap={6}>
                  <Text size="sm"><strong>Approval route:</strong> {review.task.approval_route || '-'}</Text>
                  <Text size="sm"><strong>Merge readiness:</strong> {review.task.merge_readiness || 'unknown'}</Text>
                  <Text size="sm"><strong>Follow-up:</strong> {review.follow_up?.required ? review.follow_up.comment || 'required' : 'not required'}</Text>
                  <Text size="sm"><strong>Merge summary:</strong> {review.task.merge_summary || '-'}</Text>
                  {signalTransitions.length > 0 && (
                    <Stack gap={4} mt="xs">
                      {signalTransitions.map((transition) => (
                        <Text size="xs" c="dimmed" key={`${transition.state}-${transition.action}`}>
                          {transition.state} {'->'} {transition.action} {'->'} {transition.target_phase}: {transition.summary}
                        </Text>
                      ))}
                    </Stack>
                  )}
                </Stack>
              )}
            </Paper>
          </>
        )}
      </Stack>
    </Card>

    <Modal opened={modalOpen} onClose={()=>setModalOpen(false)} title="New Task">
      <form onSubmit={form.onSubmit(submit)}>
        <Textarea label="Objective" {...form.getInputProps('objective')} required rows={3} mb="sm"/>
        <TextInput label="Repository Path" {...form.getInputProps('repoPath')} placeholder="/path/to/repo" required mb="lg"/>
        <Button type="submit" fullWidth>Submit Task</Button>
      </form>
    </Modal>
  </>;
}
