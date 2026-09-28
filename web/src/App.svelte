<script lang="ts">
  import QRCode from 'qrcode';
  import { Map as MapLibreMap, Marker, NavigationControl } from 'maplibre-gl';
  import type { GeoJSONSource } from 'maplibre-gl';
  import 'maplibre-gl/dist/maplibre-gl.css';
  import { forestryStyle, installForestryContext } from './lib/live/mapstyle';
  import { FORESTRY_BOUNDS, FORESTRY_ROUTES, worldFacilities, worldLabels } from './lib/live/world';
  type Health = { revision: string; database_configured: boolean; database_ready: boolean };
  type Device = { id:number; name:string; imei:string; model:string; status:'online'|'offline'; latitude:number; longitude:number; speed:number; heading:number; ignition:boolean; updated_at:string };
  type RoutePoint = { latitude:number; longitude:number; speed:number; heading:number };
  type DriveMode = 'auto'|'manual'|'target';
  type DriveControl = { mode:DriveMode; speed:number; heading:number; target?:{latitude:number;longitude:number}; preset?:string; paused?:boolean };
  type AuthUser={id:number;email:string;display_name:string;role:string;mfa_enabled:boolean};
  type SessionInfo={id:string;user_agent:string;ip_address:string;last_seen_at:string;created_at:string;expires_at:string};
  let user=$state<AuthUser|null>(null), csrf=$state(''), authChecked=$state(false), loginEmail=$state('admin@simulator.local'), loginPassword=$state(''), loginCode=$state(''), authError=$state('');
  let authStep=$state<'password'|'mfa'|'setup'|'recovery'>('password'), mfaQR=$state(''), setupError=$state('');
  let view=$state<'devices'|'security'|'users'>('devices'), profileOpen=$state(false), sessions=$state<SessionInfo[]>([]), users=$state<AuthUser[]>([]);
  let theme=$state<'system'|'light'|'dark'>('system'), language=$state<'id'|'en'>('en'), now=$state(new Date());
  let newUserEmail=$state(''),newUserName=$state(''),newUserRole=$state('operator'),newUserPassword=$state('');
  let currentPassword=$state(''),newPassword=$state(''),repeatPassword=$state(''),mfaSetup=$state<{secret:string;otpauth_uri:string}|null>(null),mfaCode=$state(''),recovery=$state<string[]>([]);
  async function apiFetch(url:string,init:RequestInit={}){const headers=new Headers(init.headers);if(csrf&&init.method&& !['GET','HEAD'].includes(init.method))headers.set('X-CSRF-Token',csrf);return fetch(url,{...init,headers})}
  async function loadMe(){try{const r=await fetch('/api/auth/me');if(r.ok){const p=await r.json();user=p.user;csrf=p.csrf_token||'';if(user&&!user.mfa_enabled){authStep='setup';await setupMFA()}}}finally{authChecked=true}}
  async function login(){authError='';const r=await fetch('/api/auth/login',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({email:loginEmail,password:loginPassword,code:authStep==='mfa'?loginCode:''})});if(r.status===202){authStep='mfa';loginCode='';return}if(!r.ok){authError=(await r.json().catch(()=>({error:'Sign in failed'}))).error||'Sign in failed';return}const p=await r.json();user=p.user;csrf=p.csrf_token;loginCode='';if(p.mfa_setup_required){authStep='setup';await setupMFA();return}loginPassword='';authStep='password';await refresh()}
  async function backToPassword(){authStep='password';loginCode='';authError=''}
  async function logout(){await apiFetch('/api/auth/logout',{method:'POST'});user=null;csrf='';devices=[];selected=null;profileOpen=false}
  async function openSecurity(){view='security';profileOpen=false;const r=await apiFetch('/api/security/sessions');if(r.ok)sessions=(await r.json()).items||[]}
  async function openUsers(){view='users';profileOpen=false;const r=await apiFetch('/api/admin/users');if(r.ok)users=(await r.json()).items||[]}
  async function createUser(){const r=await apiFetch('/api/admin/users',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({email:newUserEmail,displayName:newUserName,role:newUserRole,password:newUserPassword})});if(r.ok){newUserEmail='';newUserName='';newUserPassword='';await openUsers()}else message='Unable to create user'}
  async function changePassword(){if(newPassword!==repeatPassword||newPassword.length<12){message='New passwords must match and be at least 12 characters';return};const r=await apiFetch('/api/security/password',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({current:currentPassword,new:newPassword})});message=r.ok?'Password changed':'Password change failed';if(r.ok){currentPassword='';newPassword='';repeatPassword=''}}
  async function setupMFA(){setupError='';const r=await apiFetch('/api/security/mfa/setup',{method:'POST'});if(!r.ok){setupError='Unable to start authenticator setup';return}mfaSetup=await r.json();mfaQR=await QRCode.toDataURL(mfaSetup!.otpauth_uri,{width:220,margin:1,errorCorrectionLevel:'M'})}
  async function enableMFA(){setupError='';const r=await apiFetch('/api/security/mfa/enable',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({code:mfaCode})});if(!r.ok){setupError=(await r.json().catch(()=>({error:'Invalid authenticator code'}))).error||'Invalid authenticator code';return}const p=await r.json();recovery=p.recovery_codes||[];if(user)user={...user,mfa_enabled:true};mfaSetup=null;mfaCode='';mfaQR='';if(authStep==='setup')authStep='recovery'}
  async function disableMFA(){const password=prompt('Enter your current password to turn off MFA');if(!password)return;const r=await apiFetch('/api/security/mfa/disable',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({password})});if(r.ok&&user)user={...user,mfa_enabled:false}}
  async function revokeSession(id:string){const r=await apiFetch(`/api/security/sessions/${id}`,{method:'DELETE'});if(r.ok)await openSecurity()}
  async function regenerateRecovery(){const r=await apiFetch('/api/security/recovery-codes',{method:'POST'});if(r.ok)recovery=(await r.json()).recovery_codes||[]}
  function setTheme(next:'system'|'light'|'dark'){theme=next;localStorage.setItem('sim-theme',next);document.documentElement.dataset.theme=next}
  function toggleLanguage(){language=language==='en'?'id':'en';localStorage.setItem('sim-language',language)}
  function clockTime(){return now.toLocaleTimeString(language==='id'?'id-ID':'en-GB',{hour:'2-digit',minute:'2-digit',second:'2-digit',hour12:false})}
  function clockDate(){return now.toLocaleDateString(language==='id'?'id-ID':'en-US',{weekday:'long',day:'2-digit',month:'long'})}
  function liveCopy(){return devices.some(d=>d.status==='online')?(language==='id'?'baru saja':'just now'):(language==='id'?'menunggu':'waiting')}
  $effect(()=>{const saved=localStorage.getItem('sim-theme');if(saved==='system'||saved==='light'||saved==='dark')theme=saved;document.documentElement.dataset.theme=theme;const savedLanguage=localStorage.getItem('sim-language');if(savedLanguage==='id'||savedLanguage==='en')language=savedLanguage;const clock=setInterval(()=>now=new Date(),1000);return()=>clearInterval(clock)});


  function headingBetween(a:readonly [number,number],b:readonly [number,number]){
    const lat1=a[1]*Math.PI/180,lat2=b[1]*Math.PI/180,dlon=(b[0]-a[0])*Math.PI/180;
    const y=Math.sin(dlon)*Math.cos(lat2),x=Math.cos(lat1)*Math.sin(lat2)-Math.sin(lat1)*Math.cos(lat2)*Math.cos(dlon);
    return (Math.atan2(y,x)*180/Math.PI+360)%360;
  }
  const routes:RoutePoint[][]=FORESTRY_ROUTES.map(route=>route.map((point,index)=>{
    const next=route[(index+1)%route.length];
    return {latitude:point[1],longitude:point[0],speed:22+(index%4)*5,heading:headingBetween(point,next)};
  }));

  let health=$state<Health|null>(null), devices=$state<Device[]>([]), selected=$state<Device|null>(null);
  let showAdd=$state(false), saving=$state(false), message=$state('Connecting…');
  let name=$state(''), imei=$state(''), model=$state('Teltonika FMC920');
  let search=$state(''), showMapLayer=$state(true);
  let mapStage:HTMLDivElement|null=$state(null), mapContainer:HTMLDivElement|null=$state(null);
  let liveMap:MapLibreMap|null=null, liveMarker:Marker|null=null, contextMarkers:Marker[]=[] , animationFrame=0;
  let renderedPosition:{lng:number;lat:number;heading:number}|null=null, trailByDevice=new Map<number,[number,number][]>();
  let timers=new Map<number,ReturnType<typeof setInterval>>(), routeSteps=new Map<number,number>();
  let driveControls=$state(new Map<number,DriveControl>());
  let targetMarker:Marker|null=null, refreshing=$state(false);

  function routeFor(device:Device){ return routes[(device.id-1)%routes.length]; }
  function controlFor(device:Device){
    const existing=driveControls.get(device.id);if(existing)return existing;
    const created:DriveControl={mode:'auto',speed:Math.max(30,Math.round(device.speed)||40),heading:device.heading||0};driveControls.set(device.id,created);return created;
  }
  function setControl(device:Device,patch:Partial<DriveControl>){const next={...controlFor(device),...patch};driveControls.set(device.id,next);driveControls=new Map(driveControls);syncTargetMarker()}
  function destinationPoint(latitude:number,longitude:number,heading:number,speed:number,seconds=3){
    const distance=speed*1000/3600*seconds,rad=heading*Math.PI/180,latRad=latitude*Math.PI/180;
    return {latitude:latitude+(distance*Math.cos(rad))/111320,longitude:longitude+(distance*Math.sin(rad))/(111320*Math.max(.2,Math.cos(latRad))),speed,heading:(heading+360)%360};
  }
  function distanceMeters(a:{latitude:number;longitude:number},b:{latitude:number;longitude:number}){const lat=(a.latitude+b.latitude)*Math.PI/360;const dx=(b.longitude-a.longitude)*111320*Math.cos(lat),dy=(b.latitude-a.latitude)*111320;return Math.hypot(dx,dy)}
  function nextPoint(device:Device){
    const control=controlFor(device);
    if(control.mode==='auto'){const route=routeFor(device),step=routeSteps.get(device.id)??0;routeSteps.set(device.id,(step+1)%route.length);const point=route[step%route.length];return {...point,speed:control.speed}}
    if(control.mode==='target'&&!control.target)return {latitude:device.latitude,longitude:device.longitude,speed:0,heading:control.heading};
    if(control.mode==='target'&&control.target){
      const target=control.target,heading=headingBetween([device.longitude,device.latitude],[target.longitude,target.latitude]),remaining=distanceMeters(device,target),stepMeters=control.speed*1000/3600*3;
      if(remaining<=Math.max(3,stepMeters)){setControl(device,{heading,target:undefined,preset:undefined});return {latitude:target.latitude,longitude:target.longitude,speed:0,heading}}
      return destinationPoint(device.latitude,device.longitude,heading,control.speed);
    }
    return destinationPoint(device.latitude,device.longitude,control.heading,control.speed);
  }
  function syncSelected(){ if(selected) selected=devices.find(d=>d.id===selected?.id)??null; }
  function shortestHeading(from:number,to:number){return ((to-from+540)%360)-180}
  function markerElement(){const el=document.createElement('div');el.className='maplibre-device-marker';el.innerHTML='<span></span>';return el}
  function addContextMarkers(map:MapLibreMap){
    for(const marker of contextMarkers)marker.remove();contextMarkers=[];
    const add=(lng:number,lat:number,name:string,kind:'area'|'facility')=>{const el=document.createElement('div');el.className=`forestry-label ${kind}`;el.textContent=name;contextMarkers.push(new Marker({element:el,anchor:kind==='facility'?'top':'center'}).setLngLat([lng,lat]).addTo(map))};
    for(const feature of worldLabels.features){const [lng,lat]=feature.geometry.coordinates;add(lng,lat,String(feature.properties?.name??''),'area')}
    for(const feature of worldFacilities.features){const [lng,lat]=feature.geometry.coordinates;add(lng,lat,String(feature.properties?.name??''),'facility')}
  }
  function updateTrail(device:Device,lng:number,lat:number){
    const trail=trailByDevice.get(device.id)??[];const last=trail[trail.length-1];
    if(!last||Math.abs(last[0]-lng)>1e-7||Math.abs(last[1]-lat)>1e-7){trail.push([lng,lat]);if(trail.length>200)trail.splice(0,trail.length-200);trailByDevice.set(device.id,trail)}
    const source=liveMap?.getSource('trail') as GeoJSONSource|undefined;
    if(source&&trail.length>1)source.setData({type:'Feature',properties:{},geometry:{type:'LineString',coordinates:trail}});
  }
  function animateMarker(device:Device){
    if(!liveMap||!liveMarker)return;const target={lng:device.longitude,lat:device.latitude,heading:device.heading};
    if(!renderedPosition){renderedPosition=target;liveMarker.setLngLat([target.lng,target.lat]);liveMarker.setRotation(target.heading);updateTrail(device,target.lng,target.lat);return}
    cancelAnimationFrame(animationFrame);const from={...renderedPosition},started=performance.now(),duration=device.status==='online'?2800:350;
    const headingDelta=shortestHeading(from.heading,target.heading);
    const tick=(time:number)=>{const raw=Math.min(1,(time-started)/duration),t=raw<.5?2*raw*raw:1-Math.pow(-2*raw+2,2)/2;
      renderedPosition={lng:from.lng+(target.lng-from.lng)*t,lat:from.lat+(target.lat-from.lat)*t,heading:(from.heading+headingDelta*t+360)%360};
      liveMarker?.setLngLat([renderedPosition.lng,renderedPosition.lat]);liveMarker?.setRotation(renderedPosition.heading);
      if(raw<1)animationFrame=requestAnimationFrame(tick);else updateTrail(device,target.lng,target.lat)};
    animationFrame=requestAnimationFrame(tick);
  }
  function ensureMap(){
    if(!mapContainer||liveMap)return;
    const map=new MapLibreMap({container:mapContainer,style:forestryStyle(),bounds:FORESTRY_BOUNDS,fitBoundsOptions:{padding:24},attributionControl:false,maxBounds:FORESTRY_BOUNDS});
    map.addControl(new NavigationControl({showCompass:true}),'top-right');
    map.on('error',(event)=>{console.error('Live map error',event.error);message='Live map context failed to load'});
    map.on('load',()=>{installForestryContext(map);addContextMarkers(map);map.resize();map.fitBounds(FORESTRY_BOUNDS,{padding:36,duration:0});if(selected)animateMarker(selected)});
    map.on('click',(event)=>{if(!selected)return;const control=controlFor(selected);if(control.mode!=='target')return;setControl(selected,{target:{latitude:event.lngLat.lat,longitude:event.lngLat.lng},heading:headingBetween([selected.longitude,selected.latitude],[event.lngLat.lng,event.lngLat.lat]),preset:undefined});message=`Target set for ${selected.name}`});
    liveMap=map;
  }
  function syncTargetMarker(){if(targetMarker){targetMarker.remove();targetMarker=null}if(!liveMap||!selected)return;const target=controlFor(selected).target;if(!target)return;const el=document.createElement('div');el.className='target-point-marker';el.textContent='×';targetMarker=new Marker({element:el}).setLngLat([target.longitude,target.latitude]).addTo(liveMap)}
  function syncMapDevice(device:Device|null){
    ensureMap();if(!liveMap||!device)return;syncTargetMarker();
    if(!liveMarker){liveMarker=new Marker({element:markerElement(),rotationAlignment:'map',pitchAlignment:'map'}).setLngLat([device.longitude,device.latitude]).addTo(liveMap)}
    animateMarker(device);
  }
  function updatedLabel(value:string){ const date=new Date(value); return Number.isNaN(date.getTime())?'Waiting for telemetry':date.toLocaleString(); }
  function visibleDevices(){ const q=search.trim().toLowerCase(); return q?devices.filter(d=>[d.name,d.imei,d.model].some(v=>v.toLowerCase().includes(q))):devices; }
  function toggleContextLayer(){showMapLayer=!showMapLayer;if(!liveMap)return;for(const id of ['estate-fill','estate-outline','conservation-fill','conservation-outline','facility-area','roads-shadow','roads','facilities'])if(liveMap.getLayer(id))liveMap.setLayoutProperty(id,'visibility',showMapLayer?'visible':'none');for(const marker of contextMarkers)marker.getElement().style.display=showMapLayer?'':'none'}
  async function toggleMapFullscreen(){ if(!mapStage)return; if(document.fullscreenElement===mapStage) await document.exitFullscreen(); else await mapStage.requestFullscreen(); }

  async function refresh(){
    if(refreshing)return;refreshing=true;
    try{
      const hr=await fetch('/healthz'); health=await hr.json();
      if(!health?.database_ready){message='Database is not ready';return}
      const r=await apiFetch('/api/devices'); if(!r.ok) throw new Error('Unable to load devices');
      const p=await r.json(); devices=p.items??[]; syncSelected(); if(!selected&&devices.length) selected=devices[0]; message='Simulator ready';
      for(const device of devices) if(device.status==='online'&&!timers.has(device.id)&&!controlFor(device).paused) resume(device);
    }catch(e){message=e instanceof Error?e.message:'Connection failed'}finally{refreshing=false}
  }
  async function addDevice(){
    if(!name.trim()||!imei.trim()||saving)return; saving=true;
    try{
      const r=await apiFetch('/api/devices',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({name:name.trim(),imei:imei.trim(),model})});
      if(!r.ok) throw new Error('Unable to create device. Check that the IMEI / Device ID is unique.');
      name='';imei='';showAdd=false;await refresh();
    }catch(e){message=e instanceof Error?e.message:'Unable to create device'}finally{saving=false}
  }
  async function sendTelemetry(device:Device,status:'online'|'offline'){
    const point=status==='online'?nextPoint(device):{latitude:device.latitude,longitude:device.longitude,speed:0,heading:device.heading};
    const payload={status,...point,ignition:status==='online'};
    const r=await apiFetch(`/api/devices/${device.id}/telemetry`,{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify(payload)});
    if(!r.ok) throw new Error('Telemetry update failed');
    const updated:Device=await r.json(); devices=devices.map(d=>d.id===updated.id?updated:d); syncSelected();
  }
  function resume(device:Device){
    if(timers.has(device.id))return;setControl(device,{paused:false});
    const timer=setInterval(()=>{ const current=devices.find(d=>d.id===device.id); if(current) void sendTelemetry(current,'online').catch(()=>{message=`Telemetry failed for ${current.name}`}) },3000);
    timers.set(device.id,timer);
  }
  async function start(device:Device){
    if(timers.has(device.id))return;setControl(device,{paused:false});
    try{ await sendTelemetry(device,'online'); const current=devices.find(d=>d.id===device.id); if(current)resume(current); message=`${device.name} is transmitting`; }
    catch(e){message=e instanceof Error?e.message:'Unable to start simulation'}
  }
  function pause(device:Device){const timer=timers.get(device.id);if(timer)clearInterval(timer);timers.delete(device.id);setControl(device,{paused:true});message=`${device.name} paused`; }
  function setMode(device:Device,mode:DriveMode){setControl(device,{mode,target:mode==='target'?controlFor(device).target:undefined,preset:undefined,heading:device.heading});message=mode==='target'?'Click the map to choose a destination':`${device.name} switched to ${mode} drive`;}
  function setSpeed(device:Device,value:number){setControl(device,{speed:Math.min(180,Math.max(0,Math.round(value))),preset:undefined})}
  function turn(device:Device,delta:number){const c=controlFor(device);setControl(device,{mode:'manual',heading:(c.heading+delta+360)%360,target:undefined,preset:undefined})}
  function applyPreset(device:Device,preset:'normal'|'overspeed'|'drift'|'exit'|'return'){
    if(preset==='normal'||preset==='return'){setControl(device,{mode:'auto',speed:40,target:undefined,preset:undefined});message=`${device.name} returned to normal route`;return}
    if(preset==='overspeed'){setControl(device,{mode:'auto',speed:90,target:undefined,preset:'overspeed'});message=`${device.name} overspeed scenario: 90 km/h`;return}
    if(preset==='drift'){setControl(device,{mode:'manual',speed:55,heading:(device.heading+55)%360,target:undefined,preset:'drift'});message=`${device.name} drift / route deviation active`;return}
    const target={latitude:-3.032,longitude:104.892};setControl(device,{mode:'target',speed:65,heading:headingBetween([device.longitude,device.latitude],[target.longitude,target.latitude]),target,preset:'exit'});message=`${device.name} is heading outside the operating estate`;
  }
  async function stop(device:Device){
    const timer=timers.get(device.id); if(timer)clearInterval(timer); timers.delete(device.id);
    try{await sendTelemetry(device,'offline');message=`${device.name} stopped`;}catch(e){message=e instanceof Error?e.message:'Unable to stop simulation'}
  }
  function downloadSensorOnboarding(device:Device){
    void (async()=>{const r=await apiFetch(`/api/devices/${device.id}/sensor-onboarding.zip`);if(!r.ok){message='Sensor CSV download failed';return};const blob=await r.blob();const a=document.createElement('a');a.href=URL.createObjectURL(blob);a.download=`hexa-sensor-onboarding-${device.name.toLowerCase().replace(/[^a-z0-9]+/g,'-')}.zip`;a.click();URL.revokeObjectURL(a.href)})();
    message=`Hexa.Sensor onboarding CSV package downloaded for ${device.name}`;
  }
  async function remove(device:Device){
    if(device.status==='online') await stop(device);
    if(!confirm(`Delete ${device.name}? This removes the virtual device from the simulator.`))return;
    const r=await apiFetch(`/api/devices/${device.id}`,{method:'DELETE'}); if(!r.ok){message='Unable to delete device';return}
    timers.delete(device.id);routeSteps.delete(device.id);driveControls.delete(device.id);selected=null;syncTargetMarker();await refresh();message=`${device.name} deleted`;
  }
  $effect(()=>{if(mapContainer){ensureMap();syncMapDevice(selected)}});
  $effect(()=>{if(selected)syncMapDevice(selected)});
  $effect(()=>{void loadMe().then(()=>{if(user?.mfa_enabled)void refresh()}); return()=>{for(const timer of timers.values())clearInterval(timer);cancelAnimationFrame(animationFrame);for(const marker of contextMarkers)marker.remove();contextMarkers=[];liveMap?.remove();liveMap=null;liveMarker=null}});
</script>

{#if !authChecked}<div class="auth-screen"><div class="auth-loading"><div class="login-brand">Hexa.Simulator</div><p>Checking secure session…</p></div></div>{:else if !user || authStep==='setup' || authStep==='recovery'}<div class="auth-screen auth-layout"><section class="auth-hero"><div class="auth-brand"><span class="auth-logo">H+</span><strong>Hexa.Simulator</strong></div><div class="auth-hero-copy"><span class="eyebrow">Virtual telemetry. Real integration confidence.</span><h1>{authStep==='setup'?'Secure your simulator.':authStep==='recovery'?'Keep a safe way back in.':authStep==='mfa'?'A second check. A safer workspace.':'Simulate every signal before it reaches the field.'}</h1><p>{authStep==='setup'?'Connect an authenticator app before entering your workspace.':authStep==='recovery'?'Save your recovery codes somewhere secure before continuing.':'Create virtual GPS devices, stream deterministic telemetry and validate Hexa.Sensor integrations from one workspace.'}</p><div class="signal-orbit"><span>Devices</span><span>Telemetry</span><span>Sensor push</span><i></i></div></div><small>Hexa.Simulator · Secure workspace</small></section><section class="auth-panel"><div class="auth-card">{#if authStep==='password'}<span class="eyebrow">Welcome to your workspace</span><h2>Sign in</h2><p>Enter your administrator account details to continue.</p><form onsubmit={(e)=>{e.preventDefault();void login()}}><label>Email<input bind:value={loginEmail} autocomplete="username" /></label><label>Password<input type="password" bind:value={loginPassword} autocomplete="current-password" /></label>{#if authError}<div class="auth-error">{authError}</div>{/if}<button class="primary auth-submit">Continue</button></form>{:else if authStep==='mfa'}<span class="eyebrow">Identity check · Step 2 of 2</span><h2>Two-factor verification</h2><p>Enter the 6-digit code from your authenticator app, or use a recovery code.</p><form onsubmit={(e)=>{e.preventDefault();void login()}}><label>Authentication code<input class="code-input" bind:value={loginCode} autocomplete="one-time-code" inputmode="numeric" maxlength="32" placeholder="000000" autofocus /></label>{#if authError}<div class="auth-error">{authError}</div>{/if}<button class="primary auth-submit">Verify</button><button type="button" class="auth-link" onclick={()=>void backToPassword()}>Sign in as someone else</button></form>{:else if authStep==='setup'}<span class="eyebrow">Authenticator setup · Required</span><h2>Scan your QR code</h2><p>Scan this code with Google Authenticator, Microsoft Authenticator, 2FAS, or another TOTP app.</p>{#if mfaSetup}<div class="qr-wrap">{#if mfaQR}<img src={mfaQR} alt="Authenticator QR code" />{/if}<div><small>Can't scan it? Enter this setup key:</small><code>{mfaSetup.secret}</code></div></div><form onsubmit={(e)=>{e.preventDefault();void enableMFA()}}><label>6-digit authentication code<input class="code-input" bind:value={mfaCode} autocomplete="one-time-code" inputmode="numeric" maxlength="6" placeholder="000000" /></label>{#if setupError}<div class="auth-error">{setupError}</div>{/if}<button class="primary auth-submit">Verify and enable MFA</button></form>{:else}<p>Preparing secure QR code…</p>{/if}{:else}<span class="eyebrow">MFA enabled</span><h2>Save your recovery codes</h2><p>Each code can be used once if your authenticator is unavailable.</p><div class="recovery-grid">{#each recovery as code}<code>{code}</code>{/each}</div><button class="primary auth-submit" onclick={()=>{authStep='password';loginPassword='';void refresh()}}>Continue to Simulator</button>{/if}</div><div class="platform-ready"><span>●</span> Platform ready</div></section></div>{:else}<div class="shell">
  <aside>
    <div class="brand">
      <div class="mark" aria-label="Hexa.Simulator logo"><svg viewBox="0 0 32 32" aria-hidden="true"><path d="M8 2h16l7 14-7 14H8L1 16 8 2Z"/><path d="M9 10h4v4h6v-4h4v12h-4v-4h-6v4H9V10Z"/></svg></div>
      <div><strong>Hexa.Simulator</strong><span>hexa-simulator</span></div>
    </div>
    <div class="nav-section">Simulator</div>
    <nav><button class:nav-active={view==='devices'} onclick={()=>view='devices'}><span class="nav-icon">⌁</span> Devices</button></nav><div class="nav-section admin-section">Administration</div><nav><button class:nav-active={view==='users'} onclick={()=>void openUsers()}><span class="nav-icon">♙</span> Users and roles</button><button onclick={()=>document.body.classList.toggle('sidebar-collapsed')}><span class="nav-icon">◧</span> Collapse</button></nav>
    <div class="sidebar-utility">
      <div class="utility-label">Theme</div>
      <div class="theme-switch" aria-label="Theme"><button class:active={theme==='system'} onclick={()=>setTheme('system')}>System</button><button class:active={theme==='light'} onclick={()=>setTheme('light')}>Light</button><button class:active={theme==='dark'} onclick={()=>setTheme('dark')}>Dark</button></div>
      <div class="connection-state"><span class:ok={health?.database_ready}></span>{health?.database_ready?'Connected · Live':'Runtime unavailable'}</div>
      <div class="sidebar-divider"></div>
      <small>© 2026 Hexacode. All rights reserved.</small>
    </div>
  </aside>

  <div class="workspace">
    <div class="topbar">
      <div class="topbar-title"><strong>Hexa.Simulator</strong><span>Virtual GPS Device & Telemetry Simulator</span></div>
      <div class="topbar-actions"><button class="language-flag" title={language==='en'?'Switch to Bahasa Indonesia':'Switch to English'} aria-label="Change language" onclick={toggleLanguage}>{language==='en'?'🇬🇧':'🇮🇩'}</button><button class="admin-profile profile-button" onclick={()=>profileOpen=!profileOpen}>
        <div class="avatar">DA</div>
        <div class="admin-copy"><strong>{user.display_name||'Dev Administrator'}</strong><span>{user.email}</span></div>
        <span class="chevron">⌄</span>
      </button></div>{#if profileOpen}<div class="profile-menu"><button onclick={()=>void openSecurity()}>Security</button><button onclick={()=>void logout()}>Sign out</button></div>{/if}
    </div>

    {#if view==='devices'}<main class="live-main">
      <header class="live-page-head sensor-style-head">
        <div class="live-heading"><span class="live-heading-brand">Hexa.Simulator</span><h1>Live Map</h1><p class="subtitle">{language==='id'?'Pantau perangkat GPS virtual dan telemetri secara langsung.':'Monitor virtual GPS devices and telemetry in real time.'}</p></div>
        <div class="live-head-actions"><button class="refresh-button" disabled={refreshing} title="Refresh devices and telemetry" onclick={()=>void refresh()}>↻ {refreshing?'Refreshing…':'Refresh'}</button><div class="live-pill"><i></i><strong>Live</strong><span>{liveCopy()}</span></div><div class="live-clock"><strong>{clockTime()}</strong><span>{clockDate()}</span></div><button class="primary" onclick={()=>showAdd=true}>＋ {language==='id'?'Tambah perangkat':'Add device'}</button></div>
      </header>

      <section class="live-workbench" aria-label="Live simulator map">
        <aside class="live-sidebar">
          <div class="live-sidebar-head">
            <div class="live-title-row"><h2>Live Map</h2><div class="panel-tools"><button aria-label="Fullscreen map" title="Fullscreen map" onclick={()=>void toggleMapFullscreen()}>▣</button></div></div>
            <div class="live-now"><i></i><strong>Live</strong><span>{liveCopy()}</span></div>
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
              <div class="simulation-control">
                <div class="control-title"><strong>Simulation Control</strong><span>{controlFor(selected).preset||controlFor(selected).mode}</span></div>
                <div class="mode-tabs"><button class:active={controlFor(selected).mode==='auto'} onclick={()=>setMode(selected!,'auto')}>Auto Route</button><button class:active={controlFor(selected).mode==='manual'} onclick={()=>setMode(selected!,'manual')}>Manual</button><button class:active={controlFor(selected).mode==='target'} onclick={()=>setMode(selected!,'target')}>Target Point</button></div>
                <label class="speed-control"><span>Speed <b>{controlFor(selected).speed} km/h</b></span><input type="range" min="0" max="120" step="5" value={controlFor(selected).speed} oninput={(e)=>setSpeed(selected!,Number(e.currentTarget.value))}/></label>
                {#if controlFor(selected).mode==='manual'}<div class="direction-pad"><button onclick={()=>turn(selected!,-45)}>↖</button><button onclick={()=>turn(selected!,0)}>↑</button><button onclick={()=>turn(selected!,45)}>↗</button><button onclick={()=>turn(selected!,-90)}>←</button><strong>{Math.round(controlFor(selected).heading)}°</strong><button onclick={()=>turn(selected!,90)}>→</button><button onclick={()=>turn(selected!,-135)}>↙</button><button onclick={()=>turn(selected!,180)}>↓</button><button onclick={()=>turn(selected!,135)}>↘</button></div>{/if}
                {#if controlFor(selected).mode==='target'}<div class="target-hint">⌖ {controlFor(selected).target?'Destination selected — click map to change it':'Click anywhere on the map to set destination'}</div>{/if}
                <div class="scenario-grid"><button onclick={()=>applyPreset(selected!,'normal')}>Normal 40</button><button class="warn" onclick={()=>applyPreset(selected!,'overspeed')}>Overspeed 90</button><button onclick={()=>applyPreset(selected!,'drift')}>Drift</button><button onclick={()=>applyPreset(selected!,'exit')}>Exit Zone</button><button onclick={()=>applyPreset(selected!,'return')}>Return Route</button></div>
              </div>
              <div class="device-actions">
                {#if selected.status==='online'}<button class="start" disabled={!controlFor(selected).paused} onclick={()=>void start(selected!)}>▶ Resume</button><button class="pause" disabled={controlFor(selected).paused} onclick={()=>pause(selected!)}>Ⅱ Pause</button><button class="stop" onclick={()=>void stop(selected!)}>■ Stop</button>{:else}<button class="start" onclick={()=>void start(selected!)}>▶ Start</button>{/if}
                <button class="sensor-export" title="Download Hexa.Sensor import CSVs" onclick={()=>downloadSensorOnboarding(selected!)}>⇩ Sensor CSV</button>
                <button class="danger compact-danger" onclick={()=>void remove(selected!)}>Delete</button>
              </div>
            </div>
          {/if}
        </aside>

        <div class="sensor-map-stage" bind:this={mapStage}>
          <div class="maplibre-host" bind:this={mapContainer} aria-label={selected?`Live forestry map position for ${selected.name}`:'Live forestry map'}></div>
          {#if selected}
            <div class="map-device-pop"><span class="map-pop-arrow">▲</span><div><strong>{selected.name}</strong><small>{Math.round(selected.speed)} km/h · {Math.round(selected.heading)}°</small></div></div>
          {:else}
            <div class="map-empty"><span>⌁</span><strong>No device selected</strong><small>Add or select a virtual device to view its live position.</small></div>
          {/if}
          <div class="map-controls simulator-controls"><button class:control-active={showMapLayer} aria-label="Toggle map context layer" title="Toggle map context layer" onclick={toggleContextLayer}>▱</button><button aria-label="Fullscreen map" title="Fullscreen map" onclick={()=>void toggleMapFullscreen()}>⌗</button></div>
          <div class="map-legend"><span><i class="legend-operating"></i> Estate boundary</span><span><i class="legend-trail"></i> Live trail</span><span><i class="legend-device"></i> Device</span><em>Offline synthetic forestry world</em></div>
          <div class="map-runtime"><i class:ok={health?.database_ready}></i>{health?.database_ready?'Runtime ready':'Runtime unavailable'}<span>·</span><span>~3s telemetry · smooth render</span></div>
        </div>
      </section>
    </main>{:else if view==='security'}<main class="settings-main"><header><h1>Security</h1><p class="subtitle">Your password, two-factor authentication and the places you are signed in.</p></header><section class="settings-card"><h2>Two-factor authentication</h2><p>A code from your authenticator app protects every sign-in.</p><div class="setting-row"><span class:green={user.mfa_enabled}>{user.mfa_enabled?'● On':'○ Off'}</span><button class="ghost" disabled={user.mfa_enabled} onclick={()=>void setupMFA()}>Set up</button><button class="ghost" disabled={!user.mfa_enabled} onclick={()=>void regenerateRecovery()}>Regenerate recovery codes</button><button class="danger" disabled title="MFA is required for simulator accounts">Turn off</button></div>{#if mfaSetup}<div class="mfa-box"><strong>Scan with your authenticator</strong><div class="qr-wrap">{#if mfaQR}<img src={mfaQR} alt="Authenticator QR code" />{/if}<div><small>Can't scan it? Enter this setup key:</small><code>{mfaSetup.secret}</code></div></div><small>Enter the 6-digit code generated by your authenticator.</small><input bind:value={mfaCode} inputmode="numeric" maxlength="6" placeholder="123456" />{#if setupError}<div class="auth-error">{setupError}</div>{/if}<button class="primary" onclick={()=>void enableMFA()}>Enable MFA</button></div>{/if}{#if recovery.length}<div class="mfa-box"><strong>Recovery codes — save these now</strong><code>{recovery.join('  ')}</code></div>{/if}</section><section class="settings-card"><h2>Password</h2><label>Current password<input type="password" bind:value={currentPassword}/></label><label>New password<input type="password" bind:value={newPassword}/><small>At least 12 characters.</small></label><label>Repeat new password<input type="password" bind:value={repeatPassword}/></label><button class="primary" onclick={()=>void changePassword()}>Change password</button></section><section class="settings-card"><h2>Where you are signed in</h2>{#each sessions as s}<div class="session-row"><div><strong>{s.user_agent||'API tool'}</strong><small>{s.ip_address} · active {new Date(s.last_seen_at).toLocaleString()}</small></div><span>{new Date(s.expires_at).toLocaleString()} <button class="ghost mini" onclick={()=>void revokeSession(s.id)}>Revoke</button></span></div>{/each}</section></main>{:else}<main class="settings-main"><header><h1>Users and roles</h1><p class="subtitle">Accounts allowed to operate this simulator.</p></header><section class="settings-card"><h2>Add user</h2><div class="user-create"><input bind:value={newUserName} placeholder="Display name"/><input bind:value={newUserEmail} placeholder="Email"/><select bind:value={newUserRole}><option value="operator">Operator</option><option value="viewer">Viewer</option><option value="administrator">Administrator</option></select><input type="password" bind:value={newUserPassword} placeholder="Temporary password (12+ characters)"/><button class="primary" onclick={()=>void createUser()}>Create user</button></div></section><section class="settings-card"><div class="user-table"><div class="table-head"><span>User</span><span>Role</span><span>MFA</span></div>{#each users as u}<div class="table-row"><span><strong>{u.display_name}</strong><small>{u.email}</small></span><span>{u.role}</span><span class:green={u.mfa_enabled}>{u.mfa_enabled?'On':'Off'}</span></div>{/each}</div></section></main>{/if}
  </div>
</div>{/if}
{#if user && showAdd}<div class="backdrop" role="presentation" onclick={(e)=>{if(e.currentTarget===e.target)showAdd=false}}><form class="modal" onsubmit={(e)=>{e.preventDefault();void addDevice()}}><div class="modal-head"><div><p class="eyebrow">NEW SIMULATOR</p><h2>Add device</h2></div><button type="button" class="close" onclick={()=>showAdd=false}>×</button></div><label>Device name<input bind:value={name} placeholder="Truck 01" autofocus /></label><label>IMEI / Device ID<input bind:value={imei} placeholder="352093081234567" /></label><label>Device model<select bind:value={model}><option>Teltonika FMC920</option><option>Teltonika FMB920</option><option>Teltonika FMC130</option><option>Generic GPS Tracker</option></select></label><p class="hint">Each device follows a repeatable synthetic forestry haul route aligned with Hexa.Sensor and emits telemetry every 3 seconds while online.</p><div class="modal-actions"><button type="button" class="ghost" onclick={()=>showAdd=false}>Cancel</button><button class="primary" disabled={saving||!name.trim()||!imei.trim()}>{saving?'Creating…':'Create Device'}</button></div></form></div>{/if}
