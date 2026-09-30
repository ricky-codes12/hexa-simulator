import { setWorkerUrl, type ExpressionSpecification, type StyleSpecification } from 'maplibre-gl';
// ?worker bundles the worker with its ./maplibre-gl-shared.mjs import; a plain ?url copies the file
// alone and the Runtime build then 404s on the shared chunk, so no GeoJSON layer ever draws.
import maplibreWorkerUrl from 'maplibre-gl/dist/maplibre-gl-worker.mjs?worker&url';
import { STATE_COLOURS } from '../fleet/types';
import type { World } from './world';

const empty = { type: 'FeatureCollection' as const, features: [] };
setWorkerUrl(maplibreWorkerUrl);

const layer = (name: string): ExpressionSpecification => ['==', ['get', 'layer'], name];
const kind = (name: string): ExpressionSpecification => ['==', ['get', 'kind'], name];

/** Layer IDs of the estate context, which the context toggle hides and shows. */
export const CONTEXT_LAYERS = ['estate-fill', 'block-fill', 'block-line', 'zone-fill', 'zone-line', 'site-line', 'compartment-fill', 'compartment-line', 'estate-outline', 'water', 'roads-shadow', 'roads', 'public-road'];

export function forestryStyle(world: World): StyleSpecification {
  return {
    version: 8,
    // The whole world is in the initial style. Adding sources after the load event left the
    // WebGL canvas blank in some Chromium sessions.
    sources: {
      world: { type: 'geojson', data: world },
      trail: { type: 'geojson', data: empty },
      fleet: { type: 'geojson', data: empty },
    },
    layers: [
      { id: 'background', type: 'background', paint: { 'background-color': '#081116' } },
      { id: 'estate-fill', type: 'fill', source: 'world', filter: layer('estate'), paint: { 'fill-color': '#113a2d', 'fill-opacity': 0.78 } },
      { id: 'block-fill', type: 'fill', source: 'world', filter: layer('block'), paint: { 'fill-color': '#185240', 'fill-opacity': 0.55 } },
      { id: 'block-line', type: 'line', source: 'world', filter: layer('block'), paint: { 'line-color': '#24705a', 'line-width': 0.8, 'line-opacity': 0.8 } },
      { id: 'zone-fill', type: 'fill', source: 'world', filter: ['all', layer('fence'), kind('restricted')], paint: { 'fill-color': '#4a2327', 'fill-opacity': 0.8 } },
      { id: 'zone-line', type: 'line', source: 'world', filter: ['all', layer('fence'), kind('restricted')], paint: { 'line-color': '#ef6b73', 'line-width': 1.6, 'line-dasharray': [3, 2] } },
      { id: 'site-line', type: 'line', source: 'world', filter: ['all', layer('fence'), kind('site')], paint: { 'line-color': '#5b9ee9', 'line-width': 1.6 } },
      { id: 'compartment-fill', type: 'fill', source: 'world', filter: ['all', layer('fence'), kind('compartment')], paint: { 'fill-color': '#8b5cf6', 'fill-opacity': 0.28 } },
      { id: 'compartment-line', type: 'line', source: 'world', filter: ['all', layer('fence'), kind('compartment')], paint: { 'line-color': '#b39bff', 'line-width': 1.4 } },
      { id: 'estate-outline', type: 'line', source: 'world', filter: layer('estate'), paint: { 'line-color': '#9b7cff', 'line-width': 1.8, 'line-dasharray': [3, 2] } },
      { id: 'water', type: 'line', source: 'world', filter: layer('water'), paint: { 'line-color': '#3b7dd8', 'line-width': 2.2, 'line-opacity': 0.85 } },
      { id: 'roads-shadow', type: 'line', source: 'world', filter: ['all', layer('road'), ['==', ['get', 'class'], 'haul']], paint: { 'line-color': '#06100e', 'line-width': 5, 'line-opacity': 0.9 } },
      { id: 'roads', type: 'line', source: 'world', filter: ['all', layer('road'), ['==', ['get', 'class'], 'haul']], paint: { 'line-color': '#c0ad6c', 'line-width': 2.2, 'line-opacity': 0.9 } },
      { id: 'public-road', type: 'line', source: 'world', filter: ['all', layer('road'), ['==', ['get', 'class'], 'public']], paint: { 'line-color': '#cfd6e2', 'line-width': 2.6, 'line-opacity': 0.75 } },
      { id: 'trail', type: 'line', source: 'trail', paint: { 'line-color': '#18cba0', 'line-width': 2.5, 'line-opacity': 0.82 } },
      {
        id: 'fleet-dots',
        type: 'circle',
        source: 'fleet',
        paint: {
          'circle-radius': ['interpolate', ['linear'], ['zoom'], 11, 2.6, 14, 4.5, 17, 7],
          'circle-color': ['match', ['get', 'state'], 'moving', STATE_COLOURS.moving, 'idle', STATE_COLOURS.idle, 'silent', STATE_COLOURS.silent, 'stopped', STATE_COLOURS.stopped, STATE_COLOURS.parked],
          'circle-stroke-color': ['case', ['boolean', ['get', 'marked'], false], '#f4efff', '#07100e'],
          'circle-stroke-width': ['case', ['boolean', ['get', 'marked'], false], 1.6, 0.8],
        },
      },
    ],
  };
}
