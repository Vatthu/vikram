import { useEffect, useState } from 'react';
import { SimpleGrid, Card, Text, Group, Badge, Table, Title } from '@mantine/core';
import { api, DashboardData } from '../api';

export default function Dashboard() {
  const [d, setD] = useState<DashboardData | null>(null);
  useEffect(() => { api.dashboard().then(setD).catch(() => {}); }, []);
  if (!d) return <Card withBorder><Text c="dimmed">Loading...</Text></Card>;

  const stats = [
    { t:'Status', v:d.status, c:'green' }, { t:'Agents', v:d.agent_count },
    { t:'Providers', v:`${d.provider_count}/20` }, { t:'Gateway', v:`${d.gateway_host}:${d.gateway_port}` },
    { t:'Workspace', v:d.workspace?.split('/').pop() }, { t:'Sandbox', v:d.sandboxed?'enabled':'disabled' },
  ];

  const services = [
    { n:'Telegram', e:d.telegram_enabled }, { n:'WhatsApp', e:d.whatsapp_enabled },
    { n:'Heartbeat', e:d.heartbeat_enabled }, { n:'MCP', e:d.mcp_enabled },
  ];

  return <>
    <SimpleGrid cols={{base:2,sm:3,lg:6}} mb="lg">
      {stats.map(s=><Card key={s.t} withBorder><Text size="lg" fw={700} ff="'Share Tech Mono',monospace">{s.v}</Text><Text size="xs" c="dimmed" tt="uppercase">{s.t}</Text></Card>)}
    </SimpleGrid>
    <Card withBorder>
      <Title order={4} mb="md">Services</Title>
      <Table>
        <Table.Thead><Table.Tr><Table.Th>Service</Table.Th><Table.Th>Status</Table.Th></Table.Tr></Table.Thead>
        <Table.Tbody>{services.map(s=><Table.Tr key={s.n}><Table.Td>{s.n}</Table.Td><Table.Td><Badge color={s.e?'green':'gray'} variant="light">{s.e?'active':'disabled'}</Badge></Table.Td></Table.Tr>)}</Table.Tbody>
      </Table>
    </Card>
  </>;
}
