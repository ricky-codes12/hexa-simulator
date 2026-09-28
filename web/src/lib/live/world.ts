import type { FeatureCollection, LineString, Point, Polygon } from 'geojson';

export const FORESTRY_BOUNDS: [[number, number], [number, number]] = [[104.688, -3.042], [104.905, -2.928]];

export const FORESTRY_ROUTES = [
  [
    [104.758, -2.991], [104.771, -2.991], [104.784, -2.990], [104.798, -2.990],
    [104.812, -2.990], [104.827, -2.990], [104.842, -2.991], [104.858, -2.992], [104.875, -2.991]
  ],
  [
    [104.747, -3.016], [104.759, -3.006], [104.771, -2.998], [104.784, -2.990],
    [104.793, -2.978], [104.806, -2.967], [104.821, -2.962], [104.838, -2.964]
  ]
] as const;

export const worldAreas: FeatureCollection<Polygon> = {
  type: 'FeatureCollection',
  features: [
    { type:'Feature', properties:{kind:'estate',name:'Kenanga Estate'}, geometry:{type:'Polygon',coordinates:[[[104.696,-2.943],[104.756,-2.936],[104.782,-2.948],[104.778,-3.031],[104.733,-3.039],[104.699,-3.031],[104.696,-2.943]]] } },
    { type:'Feature', properties:{kind:'estate',name:'Meranti Estate'}, geometry:{type:'Polygon',coordinates:[[[104.778,-2.948],[104.846,-2.947],[104.873,-2.960],[104.870,-3.030],[104.778,-3.031],[104.778,-2.948]]] } },
    { type:'Feature', properties:{kind:'estate',name:'Sialang Concession'}, geometry:{type:'Polygon',coordinates:[[[104.846,-2.947],[104.898,-2.957],[104.901,-3.020],[104.872,-3.030],[104.870,-2.960],[104.846,-2.947]]] } },
    { type:'Feature', properties:{kind:'conservation',name:'Conservation zone'}, geometry:{type:'Polygon',coordinates:[[[104.701,-3.012],[104.715,-3.007],[104.726,-3.019],[104.722,-3.034],[104.705,-3.031],[104.701,-3.012]]] } },
    { type:'Feature', properties:{kind:'facility',name:'Nursery'}, geometry:{type:'Polygon',coordinates:[[[104.813,-2.953],[104.830,-2.953],[104.830,-2.966],[104.813,-2.966],[104.813,-2.953]]] } }
  ]
};

export const worldRoads: FeatureCollection<LineString> = {
  type:'FeatureCollection',
  features:[
    {type:'Feature',properties:{kind:'haul'},geometry:{type:'LineString',coordinates:[[104.701,-2.991],[104.730,-2.990],[104.758,-2.991],[104.784,-2.990],[104.812,-2.990],[104.842,-2.991],[104.875,-2.991],[104.898,-2.988]]}},
    {type:'Feature',properties:{kind:'haul'},geometry:{type:'LineString',coordinates:[[104.747,-3.016],[104.759,-3.006],[104.771,-2.998],[104.784,-2.990],[104.793,-2.978],[104.806,-2.967],[104.821,-2.962],[104.838,-2.964]]}},
    {type:'Feature',properties:{kind:'haul'},geometry:{type:'LineString',coordinates:[[104.784,-2.990],[104.772,-2.974],[104.762,-2.958],[104.746,-2.947]]}},
    {type:'Feature',properties:{kind:'haul'},geometry:{type:'LineString',coordinates:[[104.812,-2.990],[104.808,-3.006],[104.819,-3.019],[104.845,-3.027]]}},
    {type:'Feature',properties:{kind:'service'},geometry:{type:'LineString',coordinates:[[104.875,-2.991],[104.882,-3.005],[104.895,-3.011]]}}
  ]
};

export const worldFacilities: FeatureCollection<Point> = {
  type:'FeatureCollection',
  features:[
    {type:'Feature',properties:{name:'Mill',kind:'mill'},geometry:{type:'Point',coordinates:[104.784,-2.990]}},
    {type:'Feature',properties:{name:'Log landing',kind:'landing'},geometry:{type:'Point',coordinates:[104.875,-2.991]}},
    {type:'Feature',properties:{name:'Nursery',kind:'nursery'},geometry:{type:'Point',coordinates:[104.821,-2.960]}}
  ]
};

export const worldLabels: FeatureCollection<Point> = {
  type:'FeatureCollection',
  features:[
    {type:'Feature',properties:{name:'Kenanga Estate'},geometry:{type:'Point',coordinates:[104.720,-2.972]}},
    {type:'Feature',properties:{name:'Meranti Estate'},geometry:{type:'Point',coordinates:[104.829,-3.005]}},
    {type:'Feature',properties:{name:'Sialang Concession'},geometry:{type:'Point',coordinates:[104.872,-2.970]}},
    {type:'Feature',properties:{name:'Conservation zone'},geometry:{type:'Point',coordinates:[104.713,-3.024]}}
  ]
};
