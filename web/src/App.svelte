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
  let search=$state(''), mapZoom=$state(1), showMapLayer=$state(true);
  let mapStage:HTMLDivElement|null=$state(null);
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
  function visibleDevices(){ const q=search.trim().toLowerCase(); return q?devices.filter(d=>[d.name,d.imei,d.model].some(v=>v.toLowerCase().includes(q))):devices; }
  function zoomMap(delta:number){ mapZoom=Math.min(1.8,Math.max(1,mapZoom+delta)); }
  async function toggleMapFullscreen(){ if(!mapStage)return; if(document.fullscreenElement===mapStage) await document.exitFullscreen(); else await mapStage.requestFullscreen(); }

  async function refresh(){
    try{
      const hr=await fetch('/healthz'); health=await hr.json();
      if(!health?.database_ready){message='Database is not ready';return}
      const r=await fetch('/api/devices'); if(!r.ok) throw new Error('Unable to load devices');
      const p=await r.json(); devices=p.items??[]; syncSelected(); if(!selected&&devices.length) selected=devices[0]; message='Simulator ready';
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
      <div class="mark" aria-label="Hexa.Simulator logo"><svg viewBox="0 0 32 32" aria-hidden="true"><path d="M8 2h16l7 14-7 14H8L1 16 8 2Z"/><path d="M9 10h4v4h6v-4h4v12h-4v-4h-6v4H9V10Z"/></svg></div>
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

    <main class="live-main">
      <header class="live-page-head">
        <div><h1>Live Map</h1><p class="subtitle">Live virtual GPS telemetry for Hexa.Sensor integration testing.</p></div>
        <button class="primary" onclick={()=>showAdd=true}>＋ Add device</button>
      </header>

      <section class="live-workbench" aria-label="Live simulator map">
        <aside class="live-sidebar">
          <div class="live-sidebar-head">
            <div class="live-title-row"><h2>Live Map</h2><div class="panel-tools"><button aria-label="Fullscreen map" title="Fullscreen map" onclick={()=>void toggleMapFullscreen()}>▣</button></div></div>
            <div class="live-now"><i></i><strong>Live</strong><span>{devices.some(d=>d.status==='online')?'just now':'waiting'}</span></div>
          </div>
          <div class="live-sidebar-body">
            <label class="search-box"><span aria-hidden="true">⌕</span><input bind:value={search} aria-label="Search devices" placeholder="Name, device ID or model" /></label>
            <div class="filter-chips">
              <span class="chip moving">▲ Moving <b>{devices.filter(d=>d.status==='online'&&d.speed>0).length}</b></span>
              <span class="chip idle">● Idle <b>{devices.filter(d=>d.status==='online'&&d.speed===0).length}</b></span>
              <span class="chip parked">■ Parked <b>{devices.filter(d=>d.status==='offline').length}</b></span>
              <span class="chip reporting">◆ Reporting <b>{devices.filter(d=>d.status==='online').length}</b></span>
            </div>
            <div class="device-section-label">Devices</div>
            <div class="live-device-list">
              {#each visibleDevices() as device (device.id)}
                <button class:selected-device={selected?.id===device.id} class="live-device-row" onclick={()=>selected=device}>
                  <span class:active-arrow={device.status==='online'} class="device-arrow">▲</span>
                  <span class="device-row-copy"><strong>{device.name}</strong><small>{device.model}</small><small class="mono">{device.imei}</small></span>
                  <span class="device-row-meta"><small>{device.status==='online'?'just now':'offline'}</small><strong>{Math.round(device.speed)} <em>km/h</em></strong></span>
                </button>
              {/each}
            </div>
          </div>
          {#if selected}
            <div class="selected-telemetry">
              <div><span>Heading</span><strong>{Math.round(selected.heading)}°</strong></div>
              <div><span>Ignition</span><strong class:green={selected.ignition}>{selected.ignition?'On':'Off'}</strong></div>
              <div><span>Position</span><strong class="mono">{selected.latitude.toFixed(5)}, {selected.longitude.toFixed(5)}</strong></div>
              <div class="device-actions">
                {#if selected.status==='online'}<button class="stop" onclick={()=>void stop(selected!)}>■ Stop</button>{:else}<button class="start" onclick={()=>void start(selected!)}>▶ Start</button>{/if}
                <button class="danger compact-danger" onclick={()=>void remove(selected!)}>Delete</button>
              </div>
            </div>
          {/if}
        </aside>

        <div class="sensor-map-stage" bind:this={mapStage}>
          {#if selected}
            {@const map=mapGeometry(selected)}
            <svg class="sensor-map" class:map-zoomed={mapZoom>1} style={`--map-zoom:${mapZoom}`} viewBox={`0 0 ${map.width} ${map.height}`} preserveAspectRatio="xMidYMid meet" role="img" aria-label={`Live map position for ${selected.name}`}>
              <defs>
                <pattern id="sensorGrid" width="42" height="42" patternUnits="userSpaceOnUse"><path d="M42 0H0V42" fill="none" /></pattern>
                <linearGradient id="mapGlow" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#0b1718"/><stop offset="1" stop-color="#081016"/></linearGradient>
              </defs>
              <rect width="100%" height="100%" fill="url(#mapGlow)"/>
              {#if showMapLayer}
                <path d="M0 82 L115 70 L205 83 L310 65 L420 80 L535 74 L640 91" class="map-road major"/>
                <path d="M36 248 L90 210 L150 220 L204 190 L250 198 L308 156 L370 165 L430 140 L500 160 L590 135" class="map-road"/>
                <path d="M70 108 H570 V260 H70 Z" class="operating-area"/>
                <rect x="70" y="108" width="500" height="152" fill="url(#sensorGrid)" class="sensor-grid"/>
              {/if}
              <polyline points={map.routePoints} class="trail-shadow"/><polyline points={map.routePoints} class="sensor-trail"/>
              <circle cx={map.marker.x} cy={map.marker.y} r="15" class:marker-online={selected.status==='online'} class="marker-pulse"/>
              <g transform={`translate(${map.marker.x} ${map.marker.y}) rotate(${selected.heading})`} class="sensor-vehicle"><path d="M0 -11 L8 9 L0 5 L-8 9 Z"/></g>
              <text x="88" y="132" class="map-label">Jakarta Demo Route</text>
              <text x="455" y="238" class="map-label subtle">Hexa.Simulator</text>
            </svg>
            <div class="map-device-pop"><span class="map-pop-arrow">▲</span><div><strong>{selected.name}</strong><small>{Math.round(selected.speed)} km/h · {Math.round(selected.heading)}°</small></div></div>
          {:else}
            <div class="map-empty"><span>⌁</span><strong>No device selected</strong><small>Add or select a virtual device to view its live position.</small></div>
          {/if}
          <div class="map-controls"><button aria-label="Zoom in" title="Zoom in" onclick={()=>zoomMap(.2)} disabled={mapZoom>=1.8}>＋</button><button aria-label="Zoom out" title="Zoom out" onclick={()=>zoomMap(-.2)} disabled={mapZoom<=1}>−</button><button aria-label="Fullscreen map" title="Fullscreen map" onclick={()=>void toggleMapFullscreen()}>⌗</button><button class:control-active={showMapLayer} aria-label="Toggle map context layer" title="Toggle map context layer" onclick={()=>showMapLayer=!showMapLayer}>▱</button></div>
          <div class="map-legend"><span><i class="legend-operating"></i> Demo area</span><span><i class="legend-trail"></i> Trail</span><span><i class="legend-device"></i> Device</span><em>Virtual demo geography</em></div>
          <div class="map-runtime"><i class:ok={health?.database_ready}></i>{health?.database_ready?'Runtime ready':'Runtime unavailable'}<span>·</span><span>~3s telemetry</span></div>
        </div>
      </section>
    </main>
  </div>
</div>
{#if showAdd}<div class="backdrop" role="presentation" onclick={(e)=>{if(e.currentTarget===e.target)showAdd=false}}><form class="modal" onsubmit={(e)=>{e.preventDefault();void addDevice()}}><div class="modal-head"><div><p class="eyebrow">NEW SIMULATOR</p><h2>Add device</h2></div><button type="button" class="close" onclick={()=>showAdd=false}>×</button></div><label>Device name<input bind:value={name} placeholder="Truck 01" autofocus /></label><label>IMEI / Device ID<input bind:value={imei} placeholder="352093081234567" /></label><label>Device model<select bind:value={model}><option>Teltonika FMC920</option><option>Teltonika FMB920</option><option>Teltonika FMC130</option><option>Generic GPS Tracker</option></select></label><p class="hint">Each device follows a repeatable Jakarta demo route and emits telemetry every 3 seconds while online.</p><div class="modal-actions"><button type="button" class="ghost" onclick={()=>showAdd=false}>Cancel</button><button class="primary" disabled={saving||!name.trim()||!imei.trim()}>{saving?'Creating…':'Create Device'}</button></div></form></div>{/if}
