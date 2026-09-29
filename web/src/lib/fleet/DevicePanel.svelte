<script lang="ts">
  import { Undo2 } from 'lucide-svelte';
  import { behaviourName, clock, fleetCopy, kindName, type Lang } from './copy';
  import { OUTPUTS, STATE_COLOURS, outputLabel, type Catalog, type Output, type SimulationState } from './types';

  type Device = { id: number; name: string; imei: string; model: string; kind: string; output: Output; estate: string; latitude: number; longitude: number };

  let {
    device,
    sim,
    catalog,
    lang,
    tick,
    api,
    onchanged,
    onmessage,
  }: {
    device: Device;
    sim: SimulationState | null;
    catalog: Catalog | null;
    lang: Lang;
    tick: Date;
    api: (url: string, init?: RequestInit) => Promise<Response>;
    onchanged: () => void;
    onmessage: (text: string) => void;
  } = $props();

  const c = $derived(fleetCopy(lang));
  let busy = $state(false);

  const attrs = $derived(sim?.attributes ?? {});
  const unitState = $derived(sim?.state ?? 'stopped');
  const estateName = $derived(catalog?.estates.find((e) => e.Code === device.estate)?.Name ?? device.estate);
  const reportsDin1 = $derived(catalog?.kinds.find((k) => k.key === device.kind)?.fields.includes('din1') ?? false);
  const route = $derived(sim?.output ?? device.output);
  const delivery = $derived.by(() => {
    const outs = sim?.outputs ?? {};
    const keys = Object.keys(outs);
    if (!keys.length) return null;
    const bad = keys.find((k) => outs[k].status === 'rejected' || outs[k].status === 'error');
    const key = bad ?? keys[0];
    return { key, ...outs[key] };
  });
  const behaviours = $derived((sim?.behaviours ?? []).filter((b) => new Date(b.ends_at).getTime() > tick.getTime()));
  const sinceOK = $derived(delivery?.last_ok ? Math.max(0, (tick.getTime() - new Date(delivery.last_ok).getTime()) / 1000) : null);

  function n(key: string): number | undefined {
    const v = attrs[key];
    return typeof v === 'number' ? v : undefined;
  }
  function b(key: string): boolean | undefined {
    const v = attrs[key];
    return typeof v === 'boolean' ? v : undefined;
  }

  async function send(url: string, method: string, body: unknown): Promise<any> {
    busy = true;
    try {
      const r = await api(url, { method, headers: { 'content-type': 'application/json' }, body: JSON.stringify(body) });
      const payload = await r.json().catch(() => null);
      if (!r.ok) throw new Error(payload?.error ?? r.statusText);
      onchanged();
      return payload ?? {};
    } catch (e) {
      onmessage(e instanceof Error ? e.message : String(e));
      return null;
    } finally {
      busy = false;
    }
  }

  async function setOutput(o: Output) {
    if (o === route) return;
    if (await send(`/api/devices/${device.id}`, 'PATCH', { output: o })) onmessage(`${device.name} ${c.outputChanged} ${outputLabel(o)}`);
  }
  async function give(type: string) {
    if (await send('/api/fleet/behaviours', 'POST', { target: { ids: [device.id] }, type })) onmessage(`${behaviourName(lang, type)} ${c.given} ${device.name}`);
  }
  async function clear() {
    if (await send('/api/fleet/behaviours/clear', 'POST', { target: { ids: [device.id] } })) onmessage(`${device.name}: ${c.cleared}`);
  }
  const statusText = (s: string) => ({ ready: c.ready, sending: c.sending, rejected: c.rejected, error: c.error, silent: c.silent })[s] ?? s;
</script>

<div class="device-panel">
  <header>
    <div class="title">
      <strong>{device.name}</strong>
      <span class="state"><i style:background={STATE_COLOURS[unitState]}></i>{c[unitState]}</span>
    </div>
    <p>{kindName(lang, device.kind)} · {device.model.replace('Teltonika ', '')} · <span class="mono">{device.imei}</span> · {estateName}</p>
    {#if sim?.activity}<p class="activity"><span>{c.now}</span>{sim.activity}</p>{/if}
  </header>

  <section class="telemetry">
    <div><span>{c.speed}</span><strong>{Math.round(sim?.speed ?? 0)} km/h</strong></div>
    <div><span>{c.heading}</span><strong>{Math.round(sim?.heading ?? 0)}°</strong></div>
    <div><span>{c.ignition}</span><strong class:ok={b('ignition')}>{b('ignition') ? c.on : c.off}</strong></div>
    <div><span>{c.externalPower}</span><strong class:alert={b('power_cut')}>{n('external_voltage')?.toFixed(2) ?? '–'} V</strong></div>
    <div><span>{c.battery}</span><strong>{n('battery_voltage')?.toFixed(2) ?? '–'} V</strong></div>
    <div><span>{c.gsm}</span><strong>{n('gsm_signal') ?? '–'}<small>&thinsp;/&thinsp;5</small></strong></div>
    <div><span>{c.gnss}</span><strong>{n('gnss_status') === 1 ? c.fix : c.noFix}</strong></div>
    <div><span>{c.odometer}</span><strong class="mono">{n('odometer') !== undefined ? (n('odometer')! / 1000).toLocaleString(lang === 'id' ? 'id-ID' : 'en-US', { maximumFractionDigits: 1 }) : '–'} km</strong></div>
    {#if reportsDin1}
      <div><span>{c.implement}</span><strong class:ok={b('din1')}>{b('din1') ? c.implementDown : c.implementUp}</strong></div>
    {:else}
      <div><span>{c.powerCut}</span><strong class:alert={b('power_cut')}>{b('power_cut') ? c.yes : c.no}</strong></div>
    {/if}
    <div class="wide-tile"><span>{c.position}</span><strong class="mono">{device.latitude.toFixed(5)}, {device.longitude.toFixed(5)}</strong></div>
  </section>

  <section class="block">
    <h3>{c.output}</h3>
    <div class="segmented" role="radiogroup" aria-label={c.output}>
      {#each OUTPUTS as o}
        <button role="radio" aria-checked={route === o.key} class:on={route === o.key} disabled={busy} onclick={() => void setOutput(o.key)}>{o.short}</button>
      {/each}
    </div>
    {#if route === 'none'}
      <p class="status"><i></i>{c.notSent}</p>
    {:else if delivery}
      <p class="status {delivery.status}">
        <i></i>{outputLabel(delivery.key)}: {statusText(delivery.status)}
        {#if delivery.status === 'sending' && sinceOK !== null}<span class="mono">· {clock(sinceOK)}</span>{/if}
        {#if delivery.last_error && delivery.status !== 'sending'}<span>· {delivery.last_error}</span>{/if}
      </p>
      {#if delivery.status === 'rejected'}<p class="hint">{c.rejectedHint}</p>{/if}
    {:else}
      <p class="status"><i></i>{c.notConfigured}</p>
    {/if}
  </section>

  <section class="block">
    <h3>{c.behaviours}</h3>
    <div class="behaviours">
      {#each catalog?.behaviours ?? [] as spec}
        <button disabled={busy || !sim?.running} title={`${spec.does} Sensor: ${spec.sensor}.`} onclick={() => void give(spec.type)}>{behaviourName(lang, spec.type)}</button>
      {/each}
    </div>
    {#if behaviours.length}
      <ul class="given">
        {#each behaviours as bh (bh.id)}
          {@const starts = new Date(bh.starts_at).getTime()}
          {@const ends = new Date(bh.ends_at).getTime()}
          <li class:active={starts <= tick.getTime()}>
            <strong>{behaviourName(lang, bh.type)}</strong>
            <span>{bh.source.startsWith('scenario:') ? c.timeline : ''}</span>
            <span class="mono">{starts > tick.getTime() ? `${c.startsIn} ${clock((starts - tick.getTime()) / 1000)}` : `${c.endsIn} ${clock((ends - tick.getTime()) / 1000)}`}</span>
          </li>
        {/each}
      </ul>
      <button class="ghost wide" disabled={busy} onclick={() => void clear()}><Undo2 size={14} />{c.clear}</button>
    {:else}
      <p class="hint">{c.noBehaviours}</p>
    {/if}
  </section>
</div>

<style>
  .device-panel { display: grid; gap: 12px; padding: 12px 16px 6px; color: var(--fx-text); }
  header { display: grid; gap: 4px; }
  .title { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
  .title strong { font-size: 18px; font-weight: 650; letter-spacing: -0.01em; }
  .state { display: inline-flex; align-items: center; gap: 6px; font-size: 11.5px; color: var(--fx-muted); border: 1px solid var(--fx-border); border-radius: 6px; padding: 3px 8px; }
  .state i { width: 7px; height: 7px; border-radius: 2px; }
  header p { margin: 0; font-size: 11.5px; color: var(--fx-muted); line-height: 1.45; }
  .activity { display: flex; gap: 8px; align-items: baseline; color: var(--fx-text) !important; }
  .activity span { font-size: 10.5px; color: var(--fx-faint); text-transform: uppercase; letter-spacing: 0.06em; }
  .telemetry { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 6px; }
  .telemetry div { border: 1px solid var(--fx-border); background: var(--fx-sunken); border-radius: 7px; padding: 7px 8px; display: grid; gap: 3px; min-width: 0; }
  .telemetry span { font-size: 10.5px; color: var(--fx-muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .telemetry strong { font-size: 12.5px; font-weight: 600; font-variant-numeric: tabular-nums; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .telemetry strong small { font-weight: 400; color: var(--fx-muted); }
  .telemetry strong.alert { color: var(--fx-danger); }
  .telemetry strong.ok { color: var(--fx-ok); }
  .telemetry .wide-tile { grid-column: 1 / -1; }
  .block { border-top: 1px solid var(--fx-border); padding-top: 11px; }
  h3 { margin: 0 0 8px; font-size: 12px; font-weight: 600; }
  .segmented { display: grid; grid-template-columns: repeat(5, 1fr); border: 1px solid var(--fx-border-strong); border-radius: 8px; overflow: hidden; }
  .segmented button { border: 0; border-right: 1px solid var(--fx-border-strong); background: var(--fx-sunken); color: var(--fx-muted); font: inherit; font-size: 11.5px; font-weight: 500; padding: 7px 4px; cursor: pointer; transition: background 160ms ease-out, color 160ms ease-out; }
  .segmented button:last-child { border-right: 0; }
  .segmented button:hover:not(:disabled):not(.on) { color: var(--fx-text); }
  .segmented button.on { background: var(--fx-brand-soft); color: var(--fx-text); box-shadow: inset 0 -2px 0 var(--fx-brand); }
  .status { margin: 8px 0 0; display: flex; flex-wrap: wrap; align-items: center; gap: 5px; font-size: 12px; }
  .status i { width: 7px; height: 7px; border-radius: 50%; background: var(--fx-faint); }
  .status.sending i { background: var(--fx-ok); }
  .status.rejected i { background: var(--fx-warn); }
  .status.error i { background: var(--fx-danger); }
  .status span { color: var(--fx-muted); }
  .status.error span { color: var(--fx-danger); }
  .hint { margin: 6px 0 0; font-size: 11.5px; line-height: 1.45; color: var(--fx-muted); }
  .behaviours { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 5px; }
  .behaviours button, .ghost { font: inherit; font-size: 12px; font-weight: 500; text-align: left; border: 1px solid var(--fx-border-strong); background: var(--fx-surface); color: var(--fx-text); border-radius: 7px; padding: 7px 9px; cursor: pointer; transition: border-color 160ms ease-out, transform 80ms ease-out; }
  .behaviours button:hover:not(:disabled), .ghost:hover:not(:disabled) { border-color: var(--fx-brand); }
  .behaviours button:active:not(:disabled), .ghost:active:not(:disabled) { transform: translateY(1px); }
  button:disabled { opacity: 0.45; cursor: not-allowed; }
  button:focus-visible { outline: 2px solid var(--fx-brand); outline-offset: 1px; }
  .given { list-style: none; margin: 10px 0 0; padding: 0; display: grid; gap: 4px; }
  .given li { display: grid; grid-template-columns: minmax(0, 1fr) auto auto; gap: 8px; align-items: baseline; font-size: 12px; padding: 6px 8px; border-radius: 7px; border: 1px dashed var(--fx-border-strong); }
  .given li.active { border-style: solid; border-color: var(--fx-brand); background: var(--fx-brand-soft); }
  .given li span { font-size: 11px; color: var(--fx-muted); }
  .ghost { display: inline-flex; align-items: center; justify-content: center; gap: 6px; }
  .wide { width: 100%; margin-top: 8px; }
  .mono { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 0.95em; }
  @media (prefers-reduced-motion: reduce) { button, .segmented button { transition: none; } }
</style>
