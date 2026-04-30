import { useEffect, useState } from 'react';
import { Card, Switch, TextInput, Button, Group, Title } from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { api } from '../api';

export default function Channels() {
  const [data, setData] = useState<any>(null);
  const [tg, setTg] = useState<any>({});
  const [wa, setWa] = useState<any>({});
  useEffect(()=>{api.channels().then(d=>{setData(d);setTg({...d.telegram});setWa({...d.whatsapp})}).catch(()=>{})},[]);
  if(!data) return <Card withBorder><div style={{textAlign:'center',padding:40,color:'#888'}}>Loading...</div></Card>;

  const save=async(name:string,body:any)=>{
    try{await api.saveChannel(name,body);notifications.show({title:'Saved',message:name,color:'green'});const d=await api.channels();setData(d)}
    catch(e:any){notifications.show({title:'Error',message:e.message,color:'red'})}
  };

  const ChCard=({name,info,state,setState}:{name:string,info:any,state:any,setState:(v:any)=>void})=>(
    <Card withBorder mb="md">
      <Group justify="space-between" mb="md">
        <Title order={4}>{name==='telegram'?'Telegram':'WhatsApp'}</Title>
        <Switch checked={info.enabled} onChange={e=>save(name,{...info,enabled:e.currentTarget.checked})} label={info.enabled?'Active':'Disabled'}/>
      </Group>
      {info.token!==undefined&&<TextInput label={name==='telegram'?'Bot Token':'Bridge Token'} value={state.token} onChange={e=>setState({...state,token:e.currentTarget.value})} mb="sm"/>}
      {info.bridge_url!==undefined&&<TextInput label="Bridge URL" value={state.bridge_url} onChange={e=>setState({...state,bridge_url:e.currentTarget.value})} mb="sm"/>}
      {info.proxy!==undefined&&<TextInput label="Proxy" value={state.proxy} onChange={e=>setState({...state,proxy:e.currentTarget.value})} mb="sm" placeholder="Optional"/>}
      <Button onClick={()=>save(name,{token:state.token,enabled:info.enabled,proxy:state.proxy,bridge_url:state.bridge_url})}>Save</Button>
    </Card>
  );

  return <><ChCard name="telegram" info={data.telegram} state={tg} setState={setTg}/><ChCard name="whatsapp" info={data.whatsapp} state={wa} setState={setWa}/></>;
}
