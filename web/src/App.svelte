<script lang="ts">
  type Health = { revision: string; database_configured: boolean; database_ready: boolean };
  type Device = { id:number; name:string; imei:string; model:string; status:'online'|'offline'; latitude:number; longitude:number; speed:number; heading:number; ignition:boolean; updated_at:string };
  type RoutePoint = { latitude:number; longitude:number; speed:number; heading:number };

  const routes:RoutePoint[][] = [
    [
      {latitude:-6.20880,longitude:106.84560,speed:24,heading:88},
      {latitude:-6.20872,longitude:106.84710,speed:31,heading:91},
      {latitude:-6.20866,longitude:106.84870,speed:38,heading:89},
      {latitude:-6.20855,longitude:106.85020,speed:34,heading:86},
      {latitude:-6.20792,longitude:106.85135,speed:28,heading:42},
      {latitude:-6.20685,longitude:106.85182,speed:22,heading:18},
      {latitude:-6.20570,longitude:106.85162,speed:27,heading:350},
      {latitude:-6.20475,longitude:106.85080,speed:33,heading:318}
    ],
    [
      {latitude:-6.28455,longitude:106.82310,speed:18,heading:5},
      {latitude:-6.28335,longitude:106.82325,speed:26,heading:8},
      {latitude:-6.28205,longitude:106.82360,speed:35,heading:14},
      {latitude:-6.28090,longitude:106.82420,speed:42,heading:25},
      {latitude:-6.28010,longitude:106.82530,speed:30,heading:52},
      {latitude:-6.27985,longitude:106.82665,speed:24,heading:78},
      {latitude:-6.28020,longitude:106.82795,speed:29,heading:105},
      {latitude:-6.28110,longitude:106.82885,speed:21,heading:138}
    ]
  ];

  let health=$state<Health|null>(null), devices=$state<Device[]>([]), selected=$state<Device|null>(null);
  let showAdd=$state(false), saving=$state(false), message=$state('Connecting…');
  let name=$state(''), imei=$state(''), model=$state('Teltonika FMC920');
  let timers=new Map<number,ReturnType<typeof setInterval>>(), routeSteps=new Map<number,number>();

  function routeFor(device:Device){ return routes[(device.id-1)%routes.length]; }
  function nextPoint(device:Device){ const route=routeFor(device); const step=routeSteps.get(device.id)??0; routeSteps.set(device.id,(step+1)%route.length); return route[step%route.length]; }
  function syncSelected(){ if(selected) selected=devices.find(d=>d.id===selected?.id)??null; }
  function mapGeometry(device:Device){
    const route=routeFor(device); const points=[...route,{...route[0]}];
    const lats=points.map(p=>p.latitude), lngs=points.map(p=>p.longitude);
    const minLat=Math.min(...lats), maxLat=Math.max(...lats), minLng=Math.min(...lngs), maxLng=Math.max(...lngs);
    const pad=24, width=640, height=300, latSpan=Math.max(maxLat-minLat,.0001), lngSpan=Math.max(maxLng-minLng,.0001);
    const project=(latitude:number,longitude:number)=>({x:pad+((longitude-minLng)/lngSpan)*(width-pad*2),y:pad+((maxLat-latitude)/latSpan)*(height-pad*2)});
    const routePoints=points.map(p=>{const q=project(p.latitude,p.longitude);return `${q.x.toFixed(1)},${q.y.toFixed(1)}`}).join(' ');
    const marker=project(device.latitude,device.longitude); return {routePoints,marker,width,height};
  }
  function updatedLabel(value:string){ const date=new Date(value); return Number.isNaN(date.getTime())?'Waiting for telemetry':date.toLocaleString(); }

  async function refresh(){
    try{
      const hr=await fetch('/healthz'); health=await hr.json();
      if(!health?.database_ready){message='Database is not ready';return}
      const r=await fetch('/api/devices'); if(!r.ok) throw new Error('Unable to load devices');
      const p=await r.json(); devices=p.items??[]; syncSelected(); message='Simulator ready';
      for(const device of devices) if(device.status==='online'&&!timers.has(device.id)) resume(device);
    }catch(e){message=e instanceof Error?e.message:'Connection failed'}
  }
  async function addDevice(){
    if(!name.trim()||!imei.trim()||saving)return; saving=true;
    try{
      const r=await fetch('/api/devices',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({name:name.trim(),imei:imei.trim(),model})});
      if(!r.ok) throw new Error('Unable to create device. Check that the IMEI / Device ID is unique.');
      name='';imei='';showAdd=false;await refresh();
    }catch(e){message=e instanceof Error?e.message:'Unable to create device'}finally{saving=false}
  }
  async function sendTelemetry(device:Device,status:'online'|'offline'){
    const point=status==='online'?nextPoint(device):{latitude:device.latitude,longitude:device.longitude,speed:0,heading:device.heading};
    const payload={status,...point,ignition:status==='online'};
    const r=await fetch(`/api/devices/${device.id}/telemetry`,{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify(payload)});
    if(!r.ok) throw new Error('Telemetry update failed');
    const updated:Device=await r.json(); devices=devices.map(d=>d.id===updated.id?updated:d); syncSelected();
  }
  function resume(device:Device){
    if(timers.has(device.id))return;
    const timer=setInterval(()=>{ const current=devices.find(d=>d.id===device.id); if(current) void sendTelemetry(current,'online').catch(()=>{message=`Telemetry failed for ${current.name}`}) },3000);
    timers.set(device.id,timer);
  }
  async function start(device:Device){
    if(timers.has(device.id))return;
    try{ await sendTelemetry(device,'online'); const current=devices.find(d=>d.id===device.id); if(current)resume(current); message=`${device.name} is transmitting`; }
    catch(e){message=e instanceof Error?e.message:'Unable to start simulation'}
  }
  async function stop(device:Device){
    const timer=timers.get(device.id); if(timer)clearInterval(timer); timers.delete(device.id);
    try{await sendTelemetry(device,'offline');message=`${device.name} stopped`;}catch(e){message=e instanceof Error?e.message:'Unable to stop simulation'}
  }
  async function remove(device:Device){
    if(device.status==='online') await stop(device);
    if(!confirm(`Delete ${device.name}? This removes the virtual device from the simulator.`))return;
    const r=await fetch(`/api/devices/${device.id}`,{method:'DELETE'}); if(!r.ok){message='Unable to delete device';return}
    timers.delete(device.id);routeSteps.delete(device.id);selected=null;await refresh();message=`${device.name} deleted`;
  }
  $effect(()=>{void refresh(); return()=>{for(const timer of timers.values())clearInterval(timer)}});
</script>

<div class="shell">
  <aside>
    <div class="brand">
      <div class="mark"><span>H</span></div>
      <div><strong>Hexa.Simulator</strong><span>hexa-simulator</span></div>
    </div>
    <div class="nav-section">Simulator</div>
    <nav><button class="nav-active"><span class="nav-icon">⌁</span> Devices</button></nav>
    <div class="aside-foot"><span class:ok={health?.database_ready}></span>{health?.database_ready?'Runtime connected':'Runtime unavailable'}</div>
  </aside>

  <div class="workspace">
    <div class="topbar">
      <div class="topbar-state"><span class:ok={health?.database_ready}></span>{health?.database_ready?'Simulator ready':'Checking runtime'}</div>
      <div class="admin-profile">
        <div class="avatar">SA</div>
        <div class="admin-copy"><strong>Simulator Administrator</strong><span>Administrator</span></div>
        <span class="chevron">⌄</span>
      </div>
    </div>

    <main>
      <header>
        <div><h1>Devices</h1><p class="subtitle">Virtual GPS devices for telemetry simulation and integration testing.</p></div>
        <button class="primary" onclick={()=>showAdd=true}>＋ Add device</button>
      </header>

      <section class="stats" aria-label="Simulator summary">
        <article><span>⌁ &nbsp;Devices</span><strong>{devices.length}</strong><small>{devices.length===1?'1 virtual unit':'Virtual units registered'}</small></article>
        <article><span>◉ &nbsp;Online</span><strong>{devices.filter(d=>d.status==='online').length}</strong><small>Currently marked online</small></article>
        <article><span>⇄ &nbsp;Transmitting</span><strong>{devices.filter(d=>d.status==='online').length}</strong><small>Telemetry cadence ~3s</small></article>
        <article><span>♡ &nbsp;Runtime</span><strong class="runtime">{health?.database_ready?'Ready':'Checking'}</strong><small>{message}</small></article>
      </section>

      <section class="panel">
        <div class="panel-head">
          <div><h2>Devices</h2><p>Manage virtual Teltonika-style devices and inspect their latest telemetry.</p></div>
          <button class="ghost" onclick={()=>void refresh()}>↻ Refresh</button>
        </div>
        {#if devices.length===0}
          <div class="empty"><div class="empty-icon">⌁</div><h3>No devices yet</h3><p>Add your first virtual GPS device to begin the simulation.</p><button class="primary" onclick={()=>showAdd=true}>＋ Add device</button></div>
        {:else}
          <div class="table-wrap"><table><thead><tr><th>Device</th><th>IMEI</th><th>Model</th><th>Status</th><th>Speed</th><th>Last position</th><th></th></tr></thead><tbody>{#each devices as device (device.id)}<tr><td><button class="device-name" onclick={()=>selected=device}><span class="device-icon">⌁</span><span><strong>{device.name}</strong><small>Device #{String(device.id).padStart(4,'0')}</small></span></button></td><td class="mono">{device.imei}</td><td>{device.model}</td><td><span class:online={device.status==='online'} class="badge"><i></i>{device.status}</span></td><td>{Math.round(device.speed)} km/h</td><td class="mono">{device.latitude.toFixed(5)}, {device.longitude.toFixed(5)}</td><td><div class="actions">{#if device.status==='online'}<button class="stop" onclick={()=>void stop(device)}>Stop</button>{:else}<button class="start" onclick={()=>void start(device)}>Start</button>{/if}<button class="more" aria-label={`Open ${device.name}`} onclick={()=>selected=device}>•••</button></div></td></tr>{/each}</tbody></table></div>
        {/if}
      </section>
    </main>
  </div>
</div>
{#if showAdd}<div class="backdrop" role="presentation" onclick={(e)=>{if(e.currentTarget===e.target)showAdd=false}}><form class="modal" onsubmit={(e)=>{e.preventDefault();void addDevice()}}><div class="modal-head"><div><p class="eyebrow">NEW SIMULATOR</p><h2>Add device</h2></div><button type="button" class="close" onclick={()=>showAdd=false}>×</button></div><label>Device name<input bind:value={name} placeholder="Truck 01" autofocus /></label><label>IMEI / Device ID<input bind:value={imei} placeholder="352093081234567" /></label><label>Device model<select bind:value={model}><option>Teltonika FMC920</option><option>Teltonika FMB920</option><option>Teltonika FMC130</option><option>Generic GPS Tracker</option></select></label><p class="hint">Each device follows a repeatable Jakarta demo route and emits telemetry every 3 seconds while online.</p><div class="modal-actions"><button type="button" class="ghost" onclick={()=>showAdd=false}>Cancel</button><button class="primary" disabled={saving||!name.trim()||!imei.trim()}>{saving?'Creating…':'Create Device'}</button></div></form></div>{/if}
{#if selected}
  {@const map=mapGeometry(selected)}
  <div class="backdrop" role="presentation" onclick={(e)=>{if(e.currentTarget===e.target)selected=null}}>
    <section class="modal detail live-detail">
      <div class="modal-head"><div><p class="eyebrow">LIVE DEVICE</p><h2>{selected.name}</h2><p class="detail-sub">{selected.model} · <span class="mono">{selected.imei}</span></p></div><button class="close" onclick={()=>selected=null}>×</button></div>
      <div class="live-layout">
        <div class="map-card">
          <div class="map-head"><div><span class:online-dot={selected.status==='online'} class="live-dot"></span><strong>{selected.status==='online'?'Live telemetry':'Simulation stopped'}</strong></div><span>3 sec cadence</span></div>
          <svg class="live-map" viewBox={`0 0 ${map.width} ${map.height}`} role="img" aria-label={`Demo route and live position for ${selected.name}`}>
            <defs><pattern id="grid" width="32" height="32" patternUnits="userSpaceOnUse"><path d="M 32 0 L 0 0 0 32" fill="none" /></pattern></defs>
            <rect width="100%" height="100%" class="map-bg"/><rect width="100%" height="100%" fill="url(#grid)" class="map-grid"/>
            <polyline points={map.routePoints} class="route-shadow"/><polyline points={map.routePoints} class="route-line"/>
            <circle cx={map.marker.x} cy={map.marker.y} r="15" class:marker-online={selected.status==='online'} class="marker-pulse"/>
            <g transform={`translate(${map.marker.x} ${map.marker.y}) rotate(${selected.heading})`} class="vehicle-marker"><circle r="9"/><path d="M 0 -6 L 5 5 L 0 3 L -5 5 Z"/></g>
          </svg>
          <div class="map-foot"><span class="mono">{selected.latitude.toFixed(6)}, {selected.longitude.toFixed(6)}</span><span>Demo route · Jakarta</span></div>
        </div>
        <div class="telemetry-panel">
          <div class="telemetry-hero"><span>Speed</span><strong>{Math.round(selected.speed)}</strong><small>km/h</small></div>
          <div class="detail-grid compact"><div><span>Status</span><strong class:green={selected.status==='online'}>{selected.status}</strong></div><div><span>Ignition</span><strong>{selected.ignition?'ON':'OFF'}</strong></div><div><span>Heading</span><strong>{Math.round(selected.heading)}°</strong></div><div><span>Device ID</span><strong>#{String(selected.id).padStart(4,'0')}</strong></div><div class="wide"><span>Last telemetry</span><strong>{updatedLabel(selected.updated_at)}</strong></div></div>
        </div>
      </div>
      <div class="modal-actions split"><button class="danger" onclick={()=>void remove(selected!)}>Delete Device</button><div>{#if selected.status==='online'}<button class="stop" onclick={()=>void stop(selected!)}>■ Stop Simulation</button>{:else}<button class="primary" onclick={()=>void start(selected!)}>▶ Start Simulation</button>{/if}</div></div>
    </section>
  </div>
{/if}
