import { useState, useEffect, useRef } from 'react';
import { Card, TextInput, Button, ScrollArea, Group, Badge, Stack, Paper } from '@mantine/core';
import { IconSend } from '@tabler/icons-react';
import { api } from '../api';

interface Message { role: 'user' | 'assistant' | 'system'; content: string; ts: number }

export default function Chat({ wsLive }: { wsLive: boolean }) {
  const [messages, setMessages] = useState<Message[]>([]);
  const [input, setInput] = useState('');
  const [thinking, setThinking] = useState(false);
  const viewport = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const handler = (e: MessageEvent) => {
      try {
        const d = JSON.parse(e.data);
        if (d.type === 'chat_user') {
          setMessages(prev => [...prev, { role: 'user', content: d.data.content, ts: d.ts }]);
        } else if (d.type === 'chat_status' && d.data.status === 'thinking') {
          setThinking(true);
        } else if (d.type === 'chat_response') {
          setThinking(false);
          setMessages(prev => [...prev, { role: 'assistant', content: d.data.content, ts: d.ts }]);
        } else if (d.type === 'chat_message') {
          setThinking(false);
          setMessages(prev => [...prev, { role: 'assistant', content: d.data.content, ts: d.ts }]);
        }
      } catch {}
    };
    // Listen on the shared WebSocket via window events
    window.addEventListener('message', handler);
    // Also listen on the document for custom events from the parent WS
    return () => window.removeEventListener('message', handler);
  }, []);

  useEffect(() => { viewport.current?.scrollTo({ top: viewport.current.scrollHeight, behavior: 'smooth' }); }, [messages, thinking]);

  const send = async () => {
    if (!input.trim()) return;
    const msg = input.trim();
    setInput('');
    setMessages(prev => [...prev, { role: 'user', content: msg, ts: Date.now() }]);
    setThinking(true);
    try { await api.sendChat(msg); } catch (e: any) {
      setThinking(false);
      setMessages(prev => [...prev, { role: 'system', content: 'Error: ' + e.message, ts: Date.now() }]);
    }
  };

  return (
    <Stack h="calc(100vh - 100px)">
      <Group justify="space-between" mb="xs">
        <Badge color={wsLive ? 'green' : 'red'} variant="dot" size="lg">{wsLive ? 'Connected' : 'Offline'}</Badge>
        {thinking && <Badge color="blue" variant="light">LeVik is thinking...</Badge>}
      </Group>
      <Card withBorder style={{ flex: 1, overflow: 'hidden' }} padding="md">
        <ScrollArea h="100%" viewportRef={viewport}>
          {messages.length === 0 && (
            <div style={{ textAlign: 'center', padding: 60, color: '#888' }}>
              <div style={{ fontSize: 32, marginBottom: 16 }}>LeVik</div>
              <div style={{ fontSize: 14 }}>Your enterprise AI engineering team.<br/>Ask us anything — plan, implement, review, deploy.</div>
            </div>
          )}
          {messages.map((m, i) => (
            <Paper key={i} p="sm" mb="xs" radius="md"
              style={{
                background: m.role === 'user' ? 'var(--mantine-color-blue-9)' : m.role === 'system' ? 'var(--mantine-color-red-9)' : 'var(--mantine-color-dark-5)',
                marginLeft: m.role === 'user' ? 'auto' : 0,
                marginRight: m.role === 'user' ? 0 : 'auto',
                maxWidth: '80%',
              }}
            >
              <div style={{ fontSize: 13, lineHeight: 1.6, whiteSpace: 'pre-wrap' }}>{m.content}</div>
              <div style={{ fontSize: 10, color: '#888', marginTop: 4 }}>
                {m.role === 'user' ? 'You' : 'LeVik'} · {new Date(m.ts).toLocaleTimeString()}
              </div>
            </Paper>
          ))}
          {thinking && (
            <Paper p="sm" mb="xs" radius="md" style={{ background: 'var(--mantine-color-dark-5)', maxWidth: '80%' }}>
              <div style={{ fontSize: 13, fontStyle: 'italic', color: '#888' }}>Thinking...</div>
            </Paper>
          )}
        </ScrollArea>
      </Card>
      <Group gap="sm">
        <TextInput placeholder="Ask LeVik..." value={input} onChange={e => setInput(e.currentTarget.value)}
          onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send(); } }}
          style={{ flex: 1 }} size="md" />
        <Button onClick={send} size="md" leftSection={<IconSend size={16} />}>Send</Button>
      </Group>
    </Stack>
  );
}
