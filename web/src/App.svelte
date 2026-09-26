<script lang="ts">
  type Health = { revision: string; database_configured: boolean; database_ready: boolean };
  type Device = { id:number; name:string; imei:string; model:string; status:'online'|'offline'; latitude:number; longitude:number; speed:number; heading:number; ignition:boolean; updated_at:string };
  let health=$state<Health|null>(null), devices=$state<Device[]>([]), selected=$state<Device|null>(null);
  let showAdd=$state(false), saving=$state(false), message=$state('Connecting…');
  let name=$state(''), imei=$state(''), model=$state('Teltonika FMC920');
  let timers=new Map<number,ReturnType<typeof setInterval>>();
  async function refresh(){ try{ const hr=await fetch('/healthz'); health=await hr.json(); if(!health?.database_ready){message='Database is not ready';return} const r=await fetch('/api/devices'); if(!r.ok) throw new Error('Unable to load devices'); const p=await r.json(); devices=p.items??[]; if(selected) selected=devices.find(d=>d.id===selected?.id)??null; message='Simulator ready'; }catch(e){message=e instanceof Error?e.message:'Connection failed'} }
  async function addDevice(){ if(!name.trim()||!imei.trim()||saving)return; saving=true; try{ const r=await fetch('/api/devices',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({name:name.trim(),imei:imei.trim(),model})}); if(!r.ok) throw new Error(await r.text()); name='';imei='';showAdd=false;await refresh(); }catch(e){message=e instanceof Error?e.message:'Unable to create device'}finally{saving=false} }
  async function sendTelemetry(device:Device,status:'online'|'offline',moving=true){ const lat=device.latitude||-6.2088, lon=device.longitude||106.8456; const payload={status,latitude:status==='online'&&moving?lat+(Math.random()-.5)*.0015:lat,longitude:status==='online'&&moving?lon+(Math.random()-.5)*.0015:lon,speed:status==='online'&&moving?Math.round(20+Math.random()*55):0,heading:status==='online'?Math.round(Math.random()*359):device.heading,ignition:status==='online'}; const r=await fetch(`/api/devices/${device.id}/telemetry`,{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify(payload)}); if(!r.ok) throw new Error('Telemetry update failed'); await refresh(); }
  async function start(device:Device){ if(timers.has(device.id))return; await sendTelemetry(device,'online'); const timer=setInterval(()=>{ const current=devices.find(d=>d.id===device.id); if(current) void sendTelemetry(current,'online'); },3000); timers.set(device.id,timer); }
  async function stop(device:Device){ const timer=timers.get(device.id); if(timer)clearInterval(timer);timers.delete(device.id);await sendTelemetry(device,'offline',false); }
  $effect(()=>{void refresh(); return()=>{for(const timer of timers.values())clearInterval(timer)}});
</script>

<div class="shell">
  <aside>
    <div class="brand"><div class="mark">H</div><div><strong>Hexa</strong><span>Simulator</span></div></div>
    <nav><button class="nav-active"><span>▣</span> Device</button></nav>
    <div class="aside-foot"><span class:ok={health?.database_ready}></span>{health?.database_ready?'System connected':'System unavailable'}</div>
  </aside>
  <main>
    <header><div><p class="eyebrow">DEVICE SIMULATOR</p><h1>Devices</h1><p class="subtitle">Create virtual GPS devices and stream realistic telemetry without physical hardware.</p></div><button class="primary" onclick={()=>showAdd=true}>＋ Add Device</button></header>
    <section class="stats">
      <article><span>Total devices</span><strong>{devices.length}</strong><small>Registered simulator units</small></article>
      <article><span>Online</span><strong>{devices.filter(d=>d.status==='online').length}</strong><small>Actively transmitting</small></article>
      <article><span>Offline</span><strong>{devices.filter(d=>d.status==='offline').length}</strong><small>Simulation stopped</small></article>
      <article><span>Runtime</span><strong class="runtime">{health?.database_ready?'Ready':'Checking'}</strong><small>{message}</small></article>
    </section>
    <section class="panel">
      <div class="panel-head"><div><h2>Device list</h2><p>Virtual Teltonika-compatible units for demo and integration testing.</p></div><button class="ghost" onclick={()=>void refresh()}>↻ Refresh</button></div>
      {#if devices.length===0}
        <div class="empty"><div class="empty-icon">⌁</div><h3>No devices yet</h3><p>Add your first virtual GPS device to begin the simulation.</p><button class="primary" onclick={()=>showAdd=true}>＋ Add Device</button></div>
      {:else}
        <div class="table-wrap"><table><thead><tr><th>Device</th><th>IMEI</th><th>Model</th><th>Status</th><th>Speed</th><th>Last position</th><th></th></tr></thead><tbody>{#each devices as device (device.id)}<tr><td><button class="device-name" onclick={()=>selected=device}><span class="device-icon">⌁</span><span><strong>{device.name}</strong><small>#{String(device.id).padStart(4,'0')}</small></span></button></td><td class="mono">{device.imei}</td><td>{device.model}</td><td><span class:online={device.status==='online'} class="badge"><i></i>{device.status}</span></td><td>{Math.round(device.speed)} km/h</td><td class="mono">{device.latitude.toFixed(5)}, {device.longitude.toFixed(5)}</td><td><div class="actions">{#if device.status==='online'}<button class="stop" onclick={()=>void stop(device)}>Stop</button>{:else}<button class="start" onclick={()=>void start(device)}>Start</button>{/if}<button class="more" onclick={()=>selected=device}>•••</button></div></td></tr>{/each}</tbody></table></div>
      {/if}
    </section>
  </main>
</div>

{#if showAdd}<div class="backdrop" role="presentation" onclick={(e)=>{if(e.currentTarget===e.target)showAdd=false}}><form class="modal" onsubmit={(e)=>{e.preventDefault();void addDevice()}}><div class="modal-head"><div><p class="eyebrow">NEW SIMULATOR</p><h2>Add device</h2></div><button type="button" class="close" onclick={()=>showAdd=false}>×</button></div><label>Device name<input bind:value={name} placeholder="Truck 01" autofocus /></label><label>IMEI / Device ID<input bind:value={imei} placeholder="352093081234567" /></label><label>Device model<select bind:value={model}><option>Teltonika FMC920</option><option>Teltonika FMB920</option><option>Teltonika FMC130</option><option>Generic GPS Tracker</option></select></label><div class="modal-actions"><button type="button" class="ghost" onclick={()=>showAdd=false}>Cancel</button><button class="primary" disabled={saving||!name.trim()||!imei.trim()}>{saving?'Creating…':'Create Device'}</button></div></form></div>{/if}
{#if selected}<div class="backdrop" role="presentation" onclick={(e)=>{if(e.currentTarget===e.target)selected=null}}><section class="modal detail"><div class="modal-head"><div><p class="eyebrow">DEVICE DETAIL</p><h2>{selected.name}</h2></div><button class="close" onclick={()=>selected=null}>×</button></div><div class="detail-grid"><div><span>Status</span><strong class:green={selected.status==='online'}>{selected.status}</strong></div><div><span>Ignition</span><strong>{selected.ignition?'ON':'OFF'}</strong></div><div><span>Speed</span><strong>{Math.round(selected.speed)} km/h</strong></div><div><span>Heading</span><strong>{Math.round(selected.heading)}°</strong></div><div class="wide"><span>Position</span><strong class="mono">{selected.latitude.toFixed(6)}, {selected.longitude.toFixed(6)}</strong></div><div class="wide"><span>IMEI</span><strong class="mono">{selected.imei}</strong></div></div><div class="modal-actions">{#if selected.status==='online'}<button class="stop" onclick={()=>void stop(selected!)}>■ Stop Simulation</button>{:else}<button class="primary" onclick={()=>void start(selected!)}>▶ Start Simulation</button>{/if}</div></section></div>{/if}
