import { setWorkerUrl, type StyleSpecification } from 'maplibre-gl';
import maplibreWorkerUrl from 'maplibre-gl/dist/maplibre-gl-worker.mjs?url';
import { worldAreas, worldFacilities, worldRoads } from './world';

const emptyTrail = {type:'FeatureCollection' as const,features:[]};
const emptyFleet = {type:'FeatureCollection' as const,features:[]};
setWorkerUrl(maplibreWorkerUrl);

export function forestryStyle(): StyleSpecification {
  return {
    version: 8,
    // Keep the offline world in the initial style. Adding its sources after the
    // map load event leaves the WebGL canvas blank in some Chromium sessions,
    // even though DOM markers and controls still appear.
    sources: {
      areas: {type:'geojson',data:worldAreas},
      roads: {type:'geojson',data:worldRoads},
      facilities: {type:'geojson',data:worldFacilities},
      trail: {type:'geojson',data:emptyTrail},
      fleet: {type:'geojson',data:emptyFleet},
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
      {id:'fleet-halo',type:'circle',source:'fleet',paint:{'circle-radius':['case',['==',['get','selected'],true],8,5.5],'circle-color':['case',['==',['get','online'],true],'#18cba0','#667085'],'circle-opacity':0.18}},
      {id:'fleet-devices',type:'circle',source:'fleet',paint:{'circle-radius':['case',['==',['get','selected'],true],5,3.2],'circle-color':['case',['==',['get','selected'],true],'#c4b5fd',['==',['get','online'],true],'#18cba0','#667085'],'circle-stroke-color':'#071016','circle-stroke-width':1.2}},
    ]
  };
}
