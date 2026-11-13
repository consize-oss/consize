import type { PluginDetail } from "@/schemas";
import { useQuery } from "@tanstack/react-query";
import { ApiError } from "@/lib/api-client";
import { pluginsView, readDashboard } from "@/lib/runtime-api";
export type PluginFilters={category?:string;q?:string};
export const pluginKeys={list:(f:PluginFilters)=>["plugins","list",f] as const,detail:(id:string)=>["plugins","detail",id] as const};
export function usePlugins(filters:PluginFilters={}) {
 return useQuery({queryKey:["runtime-dashboard"],queryFn:({signal})=>readDashboard(signal),select:data=>{
  const out=pluginsView(data);
  return {...out,items:out.items.filter(x=>(!filters.category||x.category===filters.category)&&(!filters.q||[x.id,x.display_name,x.description].join(" ").toLowerCase().includes(filters.q.toLowerCase())))};
 },refetchInterval:30_000});
}
export function usePluginDetail(id:string|undefined) {
 return useQuery({queryKey:pluginKeys.detail(id??""),queryFn:async({signal}):Promise<PluginDetail>=>{
  const p=pluginsView(await readDashboard(signal)).items.find(x=>x.id===id);
  if(!p)throw new ApiError(404,{title:"Plugin not found",status:404});
  return {...p,overview:p.description,scope_stats:[],guardrail:null,credentials:null};
 },enabled:Boolean(id),refetchInterval:30_000});
}
