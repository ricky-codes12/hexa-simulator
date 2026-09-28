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
      {id:'estate-fill',type:'fill',source:'areas',filter:['==',['get','kind'],'estate'],paint:{'fill-color':'#0b211a','fill-opacity':0.62}},
      {id:'estate-outline',type:'line',source:'areas',filter:['==',['get','kind'],'estate'],paint:{'line-color':'#7d5bd7','line-width':1.6,'line-dasharray':[3,2]}},
      {id:'conservation-fill',type:'fill',source:'areas',filter:['==',['get','kind'],'conservation'],paint:{'fill-color':'#3b1d1e','fill-opacity':0.75}},
      {id:'conservation-outline',type:'line',source:'areas',filter:['==',['get','kind'],'conservation'],paint:{'line-color':'#b83f3f','line-width':1.5,'line-dasharray':[3,2]}},
      {id:'facility-area',type:'line',source:'areas',filter:['==',['get','kind'],'facility'],paint:{'line-color':'#3670a9','line-width':1.5}},
      {id:'roads-shadow',type:'line',source:'roads',paint:{'line-color':'#06100e','line-width':5,'line-opacity':0.9}},
      {id:'roads',type:'line',source:'roads',paint:{'line-color':'#6c6546','line-width':2,'line-opacity':0.58}},
      {id:'trail',type:'line',source:'trail',paint:{'line-color':'#18cba0','line-width':2.5,'line-opacity':0.82}},
      {id:'facilities',type:'circle',source:'facilities',paint:{'circle-radius':5,'circle-color':'#4f8fe8','circle-stroke-color':'#0b1520','circle-stroke-width':2}},
      {id:'facility-labels',type:'symbol',source:'facilities',layout:{'text-field':['get','name'],'text-size':11,'text-offset':[0,1.3],'text-anchor':'top'},paint:{'text-color':'#8997ad','text-halo-color':'#071016','text-halo-width':1}},
      {id:'labels',type:'symbol',source:'labels',layout:{'text-field':['get','name'],'text-size':12,'text-anchor':'center'},paint:{'text-color':'#8391a8','text-halo-color':'#071016','text-halo-width':1.2}}
    ]
  };
}
