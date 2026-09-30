import type { Feature, FeatureCollection, Geometry, Point } from 'geojson';

// Hexa.Sensor's synthetic forestry world, served by the API from internal/world so the map and
// the fleet share one source (Sensor owns the geometry; the simulator mirrors its revision).

export type WorldProps = { layer: 'estate' | 'fence' | 'block' | 'road' | 'water' | 'label'; code?: string; name?: string; kind?: string; class?: string; text?: string; estate?: string };
export type World = FeatureCollection<Geometry, WorldProps> & { bbox: [number, number, number, number]; revision: string };

export async function loadWorld(fetcher: (url: string) => Promise<Response>): Promise<World> {
  const r = await fetcher('/api/world');
  if (!r.ok) throw new Error('Unable to load the estate layout');
  return (await r.json()) as World;
}

export function worldBounds(w: World): [[number, number], [number, number]] {
  return [[w.bbox[0], w.bbox[1]], [w.bbox[2], w.bbox[3]]];
}

/** The bounds with a margin, so a unit that leaves the operating area stays on the map. */
export function panBounds(w: World, margin = 0.04): [[number, number], [number, number]] {
  return [[w.bbox[0] - margin, w.bbox[1] - margin], [w.bbox[2] + margin, w.bbox[3] + margin]];
}

export function worldLabels(w: World): Feature<Point, WorldProps>[] {
  return w.features.filter((f): f is Feature<Point, WorldProps> => f.properties.layer === 'label' && f.geometry.type === 'Point');
}
