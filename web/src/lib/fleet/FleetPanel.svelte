<script lang="ts">
  import { Download, Play, RotateCcw, Square, Undo2 } from 'lucide-svelte';
  import { behaviourName, clock, fleetCopy, kindName, type Lang } from './copy';
  import { OUTPUTS, STATE_COLOURS, outputLabel, type Catalog, type FleetView, type Target, type UnitState } from './types';

  let {
    fleet,
    catalog,
    lang,
    tick,
    fetchedAt,
    api,
    onchanged,
    onmessage,
  }: {
    fleet: FleetView | null;
    catalog: Catalog | null;
    lang: Lang;
    tick: Date;
    fetchedAt: number;
    api: (url: string, init?: RequestInit) => Promise<Response>;
    onchanged: () => void;
    onmessage: (text: string) => void;
  } = $props();

  const c = $derived(fleetCopy(lang));
  const states: UnitState[] = ['moving', 'idle', 'parked', 'silent', 'stopped'];

  let kind = $state('');
  let estate = $state('');
  let output = $state('');
  let behaviour = $state('overspeed');
  let minutes = $state(5);
  let routeTo = $state('mqtt');
  let scenarioName = $state('demo-30min');
  let busy = $state(false);
  let confirmReset = $state(false);
  let confirmTimer: ReturnType<typeof setTimeout> | undefined;

  const target = $derived<Target>({ ...(kind ? { kind } : {}), ...(estate ? { estate } : {}), ...(output ? { output } : {}) });
  const matching = $derived(
    (fleet?.units ?? []).filter((u) => (!kind || u.kind === kind) && (!estate || u.estate === estate) && (!output || u.output === output)).length,
  );
  const run = $derived(fleet?.scenario);
  const elapsed = $derived(run ? Math.min(run.length_s, run.elapsed_s + Math.max(0, (tick.getTime() - fetchedAt) / 1000)) : 0);
  const next = $derived(run?.events.find((e) => !e.fired && e.at_s > elapsed - 1));
  const spec = $derived(catalog?.behaviours.find((b) => b.type === behaviour));

  $effect(() => {
    if (spec) minutes = Math.max(1, Math.round((spec.default_duration_s || 300) / 60));
  });

  async function post(url: string, body?: unknown): Promise<any> {
    busy = true;
    try {
      const r = await api(url, { method: 'POST', headers: { 'content-type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) });
      const text = await r.text();
      const payload = text ? JSON.parse(text) : null;
      if (!r.ok) throw new Error(payload?.error ?? r.statusText);
      onchanged();
      return payload;
    } catch (e) {
      onmessage(e instanceof Error ? e.message : String(e));
      return null;
    } finally {
      busy = false;
    }
  }

  async function applyBehaviour() {
    const r = await post('/api/fleet/behaviours', { target, type: behaviour, duration_s: behaviour === 'geofence-exit' ? 0 : minutes * 60 });
    if (r) onmessage(`${behaviourName(lang, behaviour)} ${c.applied} ${r.created} ${c.units}`);
  }
  async function clearBehaviours() {
    const r = await post('/api/fleet/behaviours/clear', { target });
    if (r) onmessage(`${r.cleared} ${c.cleared}`);
  }
  async function route() {
    const r = await post('/api/fleet/output', { target, output: routeTo });
    if (r) onmessage(`${r.updated} ${c.routed} ${outputLabel(routeTo)}`);
  }
  async function reset() {
    if (!confirmReset) {
      confirmReset = true;
      clearTimeout(confirmTimer);
      confirmTimer = setTimeout(() => (confirmReset = false), 4000);
      return;
    }
    confirmReset = false;
    if (await post('/api/fleet/reset')) onmessage(c.resetDone);
  }
  async function startScenario() {
    if (await post(`/api/scenarios/${encodeURIComponent(scenarioName)}/start`)) onmessage(c.scenarioStarted);
  }
  async function stopScenario() {
    busy = true;
    try {
      const r = await api('/api/scenarios/stop', { method: 'POST' });
      if (!r.ok) throw new Error('stop');
      onchanged();
      onmessage(c.scenarioStopped);
    } catch {
      onmessage(c.scenarioStopped + ': ' + c.error);
    } finally {
      busy = false;
    }
  }
  async function exportPackage() {
    const params = new URLSearchParams(target as Record<string, string>);
    const r = await api(`/api/fleet/sensor-onboarding.zip?${params}`);
    if (!r.ok) {
      onmessage((await r.json().catch(() => ({ error: r.statusText }))).error);
      return;
    }
    const a = document.createElement('a');
    a.href = URL.createObjectURL(await r.blob());
    a.download = 'hexa-sensor-onboarding-fleet.zip';
    a.click();
    URL.revokeObjectURL(a.href);
  }
  const fmt = (n: number) => n.toLocaleString(lang === 'id' ? 'id-ID' : 'en-US');
</script>

<div class="fleet-panel">
  <section class="counts" aria-label={c.fleet}>
    {#each states as s}
      <div class="count">
        <strong><i style:background={STATE_COLOURS[s]}></i>{fmt(fleet?.counts[s] ?? 0)}</strong>
        <span>{c[s]}</span>
      </div>
    {/each}
  </section>

  <section class="block">
    <h3>{c.delivery}</h3>
    {#each fleet?.outputs ?? [] as o}
      <div class="output-row" class:off={!o.configured}>
        <i class:ok={o.configured && o.sent > 0 && !o.failed} class:warn={o.rejected > 0 && o.configured} class:bad={o.failed > 0}></i>
        <div>
          <strong>{outputLabel(o.name)}</strong>
          {#if o.configured}
            <small>{fmt(o.sent)} {c.sent} · {fmt(o.rejected)} {c.rejected} · {fmt(o.failed)} {c.failed}{o.queue > 50 ? ` · ${fmt(o.queue)} ${c.queued}` : ''}</small>
            {#if o.last_error}<small class="error-text" title={o.last_error}>{c.lastError}: {o.last_error}</small>{/if}
          {:else}
            <small>{c.notConfigured}</small>
          {/if}
        </div>
        <b>{fmt(o.devices)} <span>{c.units}</span></b>
      </div>
    {/each}
  </section>

  <section class="block">
    <h3>{c.timeline}</h3>
    {#if run}
      <div class="run-head">
        <strong>{run.title}</strong>
        <span class="mono">{clock(elapsed)} / {clock(run.length_s)}</span>
      </div>
      <div class="progress" role="progressbar" aria-valuemin="0" aria-valuemax={run.length_s} aria-valuenow={Math.round(elapsed)}>
        <span style:width={`${(100 * elapsed) / run.length_s}%`}></span>
      </div>
      {#if next}
        <p class="next"><span>{c.next} {c.inTime} <b class="mono">{clock(next.at_s - elapsed)}</b></span>{next.note}</p>
      {:else}
        <p class="next done">{c.finished}</p>
      {/if}
      <ol class="events">
        {#each run.events as e}
          <li class:fired={e.fired || e.at_s <= elapsed}><span class="mono">T+{clock(e.at_s)}</span>{e.note}</li>
        {/each}
      </ol>
      <div class="row">
        <button class="ghost" disabled={busy} onclick={() => void startScenario()}><RotateCcw size={14} />{c.restart}</button>
        <button class="ghost" disabled={busy} onclick={() => void stopScenario()}><Square size={14} />{c.stop}</button>
      </div>
    {:else}
      <p class="hint">{c.noTimeline}</p>
      <div class="row">
        <select bind:value={scenarioName} aria-label={c.timeline}>
          {#each catalog?.scenarios ?? [] as s}<option value={s.name}>{s.title}</option>{/each}
        </select>
        <button class="primary" disabled={busy || !catalog} onclick={() => void startScenario()}><Play size={14} />{c.start}</button>
      </div>
    {/if}
  </section>

  <section class="block">
    <h3>{c.group}</h3>
    <div class="filters">
      <select bind:value={kind} aria-label={c.kind}>
        <option value="">{c.anyKind}</option>
        {#each catalog?.kinds ?? [] as k}<option value={k.key}>{kindName(lang, k.key)}</option>{/each}
      </select>
      <select bind:value={estate} aria-label={c.estate}>
        <option value="">{c.anyEstate}</option>
        {#each catalog?.estates ?? [] as e}<option value={e.Code}>{e.Name}</option>{/each}
      </select>
      <select bind:value={output} aria-label={c.output}>
        <option value="">{c.anyOutput}</option>
        {#each OUTPUTS as o}<option value={o.key}>{o.label}</option>{/each}
      </select>
    </div>
    <p class="match"><b class="mono">{fmt(matching)}</b> {c.match}</p>
    <div class="row">
      <select bind:value={behaviour} aria-label={c.behaviour}>
        {#each catalog?.behaviours ?? [] as b}<option value={b.type}>{behaviourName(lang, b.type)}</option>{/each}
      </select>
      {#if behaviour !== 'geofence-exit'}
        <label class="minutes"><input type="number" min="1" max="600" bind:value={minutes} aria-label={c.minutes} /><span>{c.minutes}</span></label>
      {/if}
      <button class="primary" disabled={busy || matching === 0} onclick={() => void applyBehaviour()}>{c.apply}</button>
    </div>
    {#if spec}<p class="hint">{spec.does} <span>Sensor: {spec.sensor}.</span></p>{/if}
    <div class="row">
      <span class="label">{c.routeTo}</span>
      <select bind:value={routeTo} aria-label={c.routeTo}>
        {#each OUTPUTS as o}<option value={o.key}>{o.label}</option>{/each}
      </select>
      <button class="ghost" disabled={busy || matching === 0} onclick={() => void route()}>{c.setOutput}</button>
    </div>
    <button class="ghost wide" disabled={busy || matching === 0} onclick={() => void clearBehaviours()}><Undo2 size={14} />{c.clear}</button>
  </section>

  <section class="block">
    <div class="row">
      <button class="ghost" class:armed={confirmReset} disabled={busy} onclick={() => void reset()}><RotateCcw size={14} />{confirmReset ? c.resetAgain : c.resetT0}</button>
      <button class="ghost" disabled={busy || matching === 0} onclick={() => void exportPackage()}><Download size={14} />{c.exportSensor}</button>
    </div>
    <p class="hint">{confirmReset ? c.resetConfirm : c.exportHint}</p>
  </section>
</div>

<style>
  .fleet-panel { display: grid; gap: 14px; padding: 14px 16px 18px; color: var(--fx-text); }
  h3 { margin: 0 0 8px; font-size: 12px; font-weight: 600; color: var(--fx-text); letter-spacing: 0; }
  .counts { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 6px; }
  .count { display: grid; gap: 2px; padding: 7px 7px 8px; border: 1px solid var(--fx-border); border-radius: 8px; background: var(--fx-sunken); min-width: 0; }
  .count strong { display: flex; align-items: center; gap: 6px; font-size: 16px; font-weight: 600; font-variant-numeric: tabular-nums; letter-spacing: -0.01em; }
  .count i { width: 7px; height: 7px; border-radius: 2px; flex: none; }
  .count span { font-size: 10.5px; color: var(--fx-muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .block { border-top: 1px solid var(--fx-border); padding-top: 12px; }
  .output-row { display: grid; grid-template-columns: 8px minmax(0, 1fr) auto; gap: 9px; align-items: start; padding: 7px 0; }
  .output-row + .output-row { border-top: 1px dashed var(--fx-border); }
  .output-row > i { width: 7px; height: 7px; margin-top: 5px; border-radius: 50%; background: var(--fx-faint); }
  .output-row > i.ok { background: var(--fx-ok); }
  .output-row > i.warn { background: var(--fx-warn); }
  .output-row > i.bad { background: var(--fx-danger); }
  .output-row div { display: grid; gap: 2px; min-width: 0; }
  .output-row strong { font-size: 12.5px; font-weight: 600; }
  .output-row small { font-size: 11px; color: var(--fx-muted); font-variant-numeric: tabular-nums; }
  .output-row small.error-text { color: var(--fx-danger); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .output-row b { font-size: 13px; font-weight: 600; font-variant-numeric: tabular-nums; white-space: nowrap; }
  .output-row b span { font-weight: 400; font-size: 11px; color: var(--fx-muted); }
  .output-row.off strong, .output-row.off b { color: var(--fx-muted); }
  .run-head { display: flex; justify-content: space-between; gap: 10px; font-size: 12.5px; }
  .run-head .mono { color: var(--fx-muted); font-variant-numeric: tabular-nums; }
  .progress { height: 4px; border-radius: 4px; background: var(--fx-sunken); margin: 8px 0 10px; overflow: hidden; }
  .progress span { display: block; height: 100%; background: var(--fx-brand); transition: width 1s linear; }
  .next { margin: 0 0 8px; display: grid; gap: 2px; font-size: 12.5px; }
  .next span { font-size: 11px; color: var(--fx-muted); }
  .next b { color: var(--fx-text); font-weight: 600; }
  .next.done { color: var(--fx-muted); }
  .events { list-style: none; margin: 0 0 10px; padding: 0; display: grid; gap: 3px; max-height: 168px; overflow: auto; }
  .events li { display: grid; grid-template-columns: 52px 1fr; gap: 8px; font-size: 11.5px; color: var(--fx-text); }
  .events li .mono { color: var(--fx-muted); font-size: 11px; }
  .events li.fired { color: var(--fx-faint); }
  .row { display: flex; align-items: center; gap: 6px; margin-top: 8px; }
  .row select { flex: 1; min-width: 0; }
  .row .label { font-size: 11.5px; color: var(--fx-muted); white-space: nowrap; }
  .filters { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 6px; }
  select, input { background: var(--fx-sunken); color: var(--fx-text); border: 1px solid var(--fx-border-strong); border-radius: 7px; padding: 7px 8px; font: inherit; font-size: 12px; min-width: 0; }
  select:focus-visible, input:focus-visible, button:focus-visible { outline: 2px solid var(--fx-brand); outline-offset: 1px; }
  .minutes { display: flex; align-items: center; gap: 4px; font-size: 11px; color: var(--fx-muted); }
  .minutes input { width: 54px; font-variant-numeric: tabular-nums; }
  .match { margin: 7px 0 0; font-size: 11.5px; color: var(--fx-muted); }
  .match b { color: var(--fx-text); font-weight: 600; }
  .hint { margin: 7px 0 0; font-size: 11.5px; line-height: 1.45; color: var(--fx-muted); }
  .hint span { color: var(--fx-faint); }
  button { display: inline-flex; align-items: center; justify-content: center; gap: 6px; font: inherit; font-size: 12px; font-weight: 500; border-radius: 7px; padding: 7px 10px; cursor: pointer; transition: background 160ms ease-out, border-color 160ms ease-out, transform 80ms ease-out; white-space: nowrap; }
  button:active:not(:disabled) { transform: translateY(1px); }
  button:disabled { opacity: 0.45; cursor: not-allowed; }
  button.primary { background: var(--fx-brand-strong); border: 1px solid var(--fx-brand-strong); color: #fff; }
  button.primary:hover:not(:disabled) { background: var(--fx-brand); }
  button.ghost { background: var(--fx-surface); border: 1px solid var(--fx-border-strong); color: var(--fx-text); flex: 1; }
  button.ghost:hover:not(:disabled) { border-color: var(--fx-brand); }
  button.ghost.armed { border-color: var(--fx-danger); color: var(--fx-danger); }
  button.wide { width: 100%; margin-top: 8px; }
  .mono { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; }
  @media (prefers-reduced-motion: reduce) { .progress span, button { transition: none; } }
</style>
