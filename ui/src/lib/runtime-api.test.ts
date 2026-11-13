import { it } from "node:test";
import assert from "node:assert/strict";
import { runtimeDashboardSchema, recommendationViews, pluginsView, summaryView, unavailableMoney } from "./runtime-api";
import { formatMoney } from "./format";
const empty=()=>runtimeDashboardSchema.parse({resources:[],recommendations:[],jobs:[],plugins:[],verification:{enabled:false,metrics_plugin_id:""},policy:{id:"oss-foundation-v1",name:"OSS",mode:"Production approval required",verification:"When configured",approval_model:"Review then approve"}});
it("normalizes Go nil collections without creating operational rows",()=>{
 const data=runtimeDashboardSchema.parse({...empty(),resources:null,recommendations:null,jobs:null,plugins:null});
 assert.deepEqual(recommendationViews(data).items,[]);
 assert.equal(pluginsView(data).summary.counts.installed,0);
 assert.equal(summaryView(data,{}).pending_approvals_mine,null);
});
it("missing pricing is unavailable, never a measured zero-dollar saving",()=>{
 assert.deepEqual(summaryView(empty(),{}).projected_savings,unavailableMoney);
 assert.equal(formatMoney(unavailableMoney).display,"—");
});
it("runtime model rejects schema drift instead of rendering fallback rows",()=>{
 assert.equal(runtimeDashboardSchema.safeParse({...empty(),resources:[{id:"broken"}]}).success,false);
 assert.equal(runtimeDashboardSchema.safeParse({...empty(),jobs:[{id:1,state:"verified"}]}).success,false);
});
it("plugin connectivity is not telemetry freshness or package signature verification",()=>{
 const data=empty();
 data.plugins.push({manifest:{id:"test",display_name:"Test",version:"1",category:"metrics",supported_resource_types:[],supported_action_types:[],capabilities:["metrics.read"],can_mutate_infrastructure:false,requires_approval:false},health:{status:"healthy",message:"API reachable",checked_at:new Date().toISOString()}});
 const view=pluginsView(data);
 assert.equal(view.items[0].health,"healthy");
 assert.equal(view.summary.ingestion.fresh_ratio,null);
 assert.equal(view.summary.trust.assessed,false);
 assert.equal(view.items[0].source,"unknown");
});
