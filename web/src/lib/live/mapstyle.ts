import type { StyleSpecification } from 'maplibre-gl';
import { worldAreas, worldFacilities, worldLabels, worldRoads } from './world';

export function forestryStyle(): StyleSpecification {
  return {
    version: 8,
    sources: {
      areas: { type:'geojson', data: worldAreas },
      roads: { type:'geojson', data: worldRoads },
      facilities: { type:'geojson', data: worldFacilities },
      labels: { type:'geojson', data: worldLabels },
      trail: { type:'geojson', data:{type:'Feature',properties:{},geometry:{type:'LineString',coordinates:[]}} }
    },
    layers: [
      {id:'background',type:'background',paint:{'background-color':'#071016'}},
      {id:'estate-fill',type:'fill',source:'areas',filter:['==',['get','kind'],'estate'],paint:{'fill-color':'#123c2f','fill-opacity':0.72}},
      {id:'estate-outline',type:'line',source:'areas',filter:['==',['get','kind'],'estate'],paint:{'line-color':'#9b7cff','line-width':2,'line-dasharray':[3,2]}},
      {id:'conservation-fill',type:'fill',source:'areas',filter:['==',['get','kind'],'conservation'],paint:{'fill-color':'#4a2327','fill-opacity':0.82}},
      {id:'conservation-outline',type:'line',source:'areas',filter:['==',['get','kind'],'conservation'],paint:{'line-color':'#ef6b73','line-width':2,'line-dasharray':[3,2]}},
      {id:'facility-area',type:'line',source:'areas',filter:['==',['get','kind'],'facility'],paint:{'line-color':'#4f9df0','line-width':2}},
      {id:'roads-shadow',type:'line',source:'roads',paint:{'line-color':'#06100e','line-width':5,'line-opacity':0.9}},
      {id:'roads',type:'line',source:'roads',paint:{'line-color':'#b4a36a','line-width':2.4,'line-opacity':0.88}},
      {id:'trail',type:'line',source:'trail',paint:{'line-color':'#18cba0','line-width':2.5,'line-opacity':0.82}},
      {id:'facilities',type:'circle',source:'facilities',paint:{'circle-radius':5,'circle-color':'#4f8fe8','circle-stroke-color':'#0b1520','circle-stroke-width':2}},
    ]
  };
}
