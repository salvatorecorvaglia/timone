import { test } from 'node:test';
import assert from 'node:assert/strict';
import { reviewReport } from '../../scripts/audit-desktop.mjs';
const advisory = () => ({github_advisory_id:'GHSA-hp3w-g68c-fv3c',module_name:'sprintf-js',severity:'moderate',patched_versions:null,findings:[{version:'1.1.3',dev:true,paths:['.>electron-builder>app-builder-lib>sprintf-js']}]});
const report = value => ({metadata:{},advisories:{one:value}});
test('only permits the exact unpatched development packaging advisory',()=>{
 assert.equal(reviewReport(report(advisory()))[0].tracked,true);
 for(const change of [a=>a.github_advisory_id='new',a=>a.severity='high',a=>a.patched_versions='>=1.1.4',a=>a.findings[0].dev=false,a=>a.findings[0].paths=['.>runtime>sprintf-js'],a=>a.findings=[]]){
  const value=advisory();change(value);assert.equal(reviewReport(report(value))[0].tracked,false);
 }
 assert.deepEqual(reviewReport({metadata:{},advisories:{}}),[]);
 assert.throws(()=>reviewReport({error:{message:'offline'}}));
});
