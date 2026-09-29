// Shapes of the fleet API (internal/httpapi/simulation.go and server.go).

export type Output = 'mqtt' | 'teltonika' | 'http-push' | 'all' | 'none';
export type UnitState = 'moving' | 'idle' | 'parked' | 'silent' | 'stopped';

export type FleetUnit = {
  id: number;
  name: string;
  kind: string;
  output: Output;
  estate: string;
  lat: number;
  lon: number;
  heading: number;
  speed: number;
  state: UnitState;
  delivery: 'ready' | 'sending' | 'rejected' | 'error' | 'silent' | 'off';
  behaviours?: string[];
};

export type OutputSummary = {
  name: 'mqtt' | 'teltonika' | 'http-push';
  configured: boolean;
  devices: number;
  sent: number;
  failed: number;
  rejected: number;
  last_ok?: string;
  last_error?: string;
  queue: number;
};

export type ScenarioEvent = { at_s: number; type?: string; note: string; fired: boolean };

export type ScenarioRun = {
  name: string;
  title: string;
  started_at: string;
  elapsed_s: number;
  length_s: number;
  finished: boolean;
  events: ScenarioEvent[];
};

export type FleetView = {
  epoch: string;
  now: string;
  interval_s: number;
  counts: Partial<Record<UnitState | 'total', number>>;
  kinds: Record<string, number>;
  outputs: OutputSummary[];
  scenario?: ScenarioRun;
  units: FleetUnit[];
};

export type BehaviourParam = { key: string; label: string; unit?: string; default: number | string; min?: number; max?: number };
export type BehaviourSpec = { type: string; name: string; does: string; sensor: string; default_duration_s: number; params: BehaviourParam[] };

export type Catalog = {
  kinds: { key: string; name: string; code: string; asset_type: string; fields: string[] }[];
  behaviours: BehaviourSpec[];
  scenarios: { name: string; title: string; summary: string; length_s: number; events: ScenarioEvent[] }[];
  estates: { Code: string; Name: string }[];
  outputs: Record<'mqtt' | 'teltonika' | 'http-push', boolean>;
};

export type OutputState = { configured: boolean; status: 'ready' | 'sending' | 'rejected' | 'error' | 'silent'; last_ok?: string; last_error?: string };

export type Behaviour = {
  id: number;
  device_id: number;
  type: string;
  name: string;
  params: Record<string, number | string>;
  starts_at: string;
  ends_at: string;
  source: string;
  active: boolean;
};

export type SimulationState = {
  device_id: number;
  running: boolean;
  paused: boolean;
  mode: 'auto' | 'manual' | 'target';
  speed: number;
  heading: number;
  output: Output;
  state: UnitState;
  activity: string;
  outputs?: Record<string, OutputState>;
  behaviours: Behaviour[];
  attributes?: Record<string, number | boolean>;
  last_error?: string;
};

export type Target = { ids?: number[]; names?: string[]; kind?: string; estate?: string; output?: string; limit?: number };

export const OUTPUTS: { key: Output; label: string; short: string }[] = [
  { key: 'mqtt', label: 'MQTT', short: 'MQTT' },
  { key: 'teltonika', label: 'Teltonika TCP', short: 'TCP' },
  { key: 'http-push', label: 'HTTP push', short: 'HTTP' },
  { key: 'all', label: 'All outputs', short: 'All' },
  { key: 'none', label: 'Silent', short: 'None' },
];

export function outputLabel(key: string): string {
  return OUTPUTS.find((o) => o.key === key)?.label ?? key;
}

// Colours of unit states on the map and in counters, one family with the console's palette.
export const STATE_COLOURS: Record<UnitState, string> = {
  moving: '#2bc99b',
  idle: '#e8a33d',
  parked: '#8a95a9',
  silent: '#ee5d72',
  stopped: '#4a5366',
};
