import { useEffect, useState } from 'react';
import { Card, Switch, TextInput, PasswordInput, Button, Group, Title } from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { api } from '../api';

export default function Channels() {
  const [data, setData] = useState<any>(null);
  const [tg, setTg] = useState<any>({});
  const [wa, setWa] = useState<any>({});
  useEffect(()=>{
    api.channels().then(d=>{setData(d);setTg({...d.telegram,token:''});setWa({...d.whatsapp,bridge_token:''})}).catch(()=>{})
  },[]);
  if(!data) return <Card withBorder><div style={{textAlign:'center',padding:40,color:'#888'}}>Loading...</div></Card>;

  const save=async(name:string,body:any)=>{
    try{await api.saveChannel(name,body);notifications.show({title:'Saved',message:name,color:'green'});const d=await api.channels();setData(d);setTg({...d.telegram,token:''});setWa({...d.whatsapp,bridge_token:''})}
    catch(e:any){notifications.show({title:'Error',message:e.message,color:'red'})}
  };

  const pairTelegram = async (otp: string) => {
    if (!otp) return;
    try {
      const r = await api.pairTelegram(otp);
      notifications.show({ title: 'Paired', message: `User ${r.username} (${r.chat_id})`, color: 'green' });
      const d = await api.channels();
      setData(d);
      setTg({...d.telegram, token: '', otp: ''});
    } catch (e: any) { notifications.show({ title: 'Pairing failed', message: e.message, color: 'red' }); }
  };

  return <>
    <Card withBorder mb="md">
      <Group justify="space-between" mb="md">
        <Title order={4}>Telegram</Title>
        <Switch checked={data.telegram.enabled} onChange={e => save('telegram', { ...data.telegram, enabled: e.currentTarget.checked })} label={data.telegram.enabled ? 'Active' : 'Disabled'} />
      </Group>
      <PasswordInput label="Bot Token" description="Leave blank to keep current token." value={tg.token || ''} onChange={e => setTg({ ...tg, token: e.currentTarget.value })} mb="sm" />
      <TextInput label="Proxy URL" value={tg.proxy||data.telegram.proxy||''} onChange={e => setTg({...tg,proxy:e.currentTarget.value})} mb="sm" />
      <TextInput label="Pairing OTP" description="From 'levik telegram pairing' output" value={tg.otp || ''} onChange={e => setTg({ ...tg, otp: e.currentTarget.value })} mb="sm" />
      <Group>
        <Button size="compact-sm" variant="light" color="green" onClick={() => pairTelegram(tg.otp)} disabled={!tg.otp}>Pair Device</Button>
        <Button onClick={() => save('telegram', { enabled: data.telegram.enabled, ...(tg.token ? { token: tg.token } : {}), proxy: tg.proxy || data.telegram.proxy || '' })}>Save</Button>
      </Group>
    </Card>

    <Card withBorder>
      <Group justify="space-between" mb="md">
        <Title order={4}>WhatsApp</Title>
        <Switch checked={data.whatsapp.enabled} onChange={e => save('whatsapp', { ...data.whatsapp, enabled: e.currentTarget.checked })} label={data.whatsapp.enabled ? 'Active' : 'Disabled'} />
      </Group>
      <PasswordInput label="Bridge Token" description="Leave blank to keep current token." value={wa.bridge_token || ''} onChange={e => setWa({ ...wa, bridge_token: e.currentTarget.value })} mb="sm" />
      <TextInput label="Bridge URL" value={wa.bridge_url||data.whatsapp.bridge_url||''} onChange={e => setWa({...wa,bridge_url:e.currentTarget.value})} mb="sm" />
      <Button onClick={() => save('whatsapp', { enabled: data.whatsapp.enabled, ...(wa.bridge_token ? { bridge_token: wa.bridge_token } : {}), bridge_url: wa.bridge_url || data.whatsapp.bridge_url || '' })}>Save</Button>
    </Card>
  </>;
}
