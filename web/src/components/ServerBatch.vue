<script setup lang="ts">
import { computed, onUnmounted, ref } from 'vue';
import { ElButton, ElDialog, ElInput, ElDropdown, ElDropdownMenu, ElDropdownItem } from 'element-plus';
import { HTTPError, requestJSON } from '../lib/api';
import 'element-plus/es/components/dropdown/style/css';
import 'element-plus/es/components/dropdown-menu/style/css';
import 'element-plus/es/components/dropdown-item/style/css';
type Row=Record<string,any>;
type Action='update'|'group'|'revoke'|'delete';
type Result={id:string;name:string;state:string;message:string};
const props=defineProps<{rows:Row[];admin:boolean;disabled:boolean}>();
const emit=defineEmits<{busy:[value:boolean];changed:[];clear:[]}>();
const names={update:'批量更新 Agent',group:'批量修改分组',revoke:'批量撤销凭证',delete:'批量删除服务器'};
const visible=ref(false),running=ref(false),finished=ref(false),action=ref<Action>('update'),group=ref('');
const targets=ref<Row[]>([]),results=ref<Result[]>([]);
const controller=new AbortController();
onUnmounted(()=>controller.abort());
const descriptions={update:'为符合条件的服务器创建更新任务。Agent 将在下一次检查时更新，期间采集会短暂重启。',group:'只修改所选服务器的分组；留空会清除分组。',revoke:'所选服务器的 Agent 凭证将失效，后续上报会被拒绝，需要重新安装注册才能恢复。',delete:'永久删除所选服务器及其登录事件、告警和投递记录，并使凭证失效。操作审计保留；目标机器上的 Agent 需另行卸载。此操作无法撤销。'};
function skip(row:Row):string {
 if(action.value==='revoke')return row.status==='revoked'?'凭证已撤销':'';
 if(action.value!=='update')return '';
 if(row.status==='revoked')return '凭证已撤销';
 if(row.available_agent_version&&row.agent_version===row.available_agent_version)return 'Agent 已是当前版本';
 if(['pending','running'].includes(row.update_status)&&new Date(row.update_expires_at).getTime()>Date.now())return '已有更新任务';
 const seen=new Date(row.updater_seen_at).getTime();
 if(!row.updater_seen_at||!Number.isFinite(seen)||Date.now()-seen>=180000)return '维护服务未就绪，请先手动更新';
 return '';
}
const summary=computed(()=>`成功 ${results.value.filter(r=>r.state==='成功').length} · 失败 ${results.value.filter(r=>r.state==='失败').length} · 未确认 ${results.value.filter(r=>r.state==='未确认').length} · 跳过 ${results.value.filter(r=>r.state==='跳过').length}`);
function open(value:Action){
 if(props.disabled||running.value||!props.rows.length||(!props.admin&&value!=='group'))return;
 action.value=value;targets.value=props.rows.map(row=>({...row}));group.value='';results.value=[];finished.value=false;visible.value=true;
}
async function execute(){
 if(running.value||finished.value||(!props.admin&&action.value!=='group'))return;
 running.value=true;emit('busy',true);
 let stopped='';
 try{
  for(const row of targets.value){
   if(controller.signal.aborted)break;
   const reason=stopped||skip(row);
   if(reason){results.value.push({id:row.id,name:row.name,state:'跳过',message:reason});continue;}
   try{
    const suffix=action.value==='delete'?'':`/${action.value}`;
    await requestJSON(`/servers/${encodeURIComponent(row.id)}${suffix}`,action.value==='group'?'PATCH':action.value==='delete'?'DELETE':'POST',action.value==='group'?{group_name:group.value}:action.value==='delete'?undefined:{},controller.signal);
    results.value.push({id:row.id,name:row.name,state:'成功',message:action.value==='update'?'更新任务已创建，实际进度请查看服务器状态':'已完成'});
   }catch(e){
    const uncertain=!(e instanceof HTTPError)||(e.status>=500||e.status<400);
    results.value.push({id:row.id,name:row.name,state:uncertain?'未确认':'失败',message:uncertain?'未收到可靠结果，请刷新服务器状态或核对操作审计后再重试。'+(e as Error).message:(e as Error).message});
    if(uncertain)stopped='连接异常，后续操作未执行';
    else if(e instanceof HTTPError&&(e.status===401||e.status===403))stopped='登录状态或权限已变化，未执行';
   }
  }
 }finally{running.value=false;finished.value=true;emit('busy',false);emit('clear');emit('changed');}
}
</script>
<template>
 <div class="batch-controls">
  <el-dropdown trigger="click" :disabled="disabled||!rows.length||running" @command="open">
   <el-button :disabled="disabled||!rows.length||running">批量操作 ▾</el-button>
   <template #dropdown><el-dropdown-menu>
    <el-dropdown-item v-if="admin" command="update">批量更新 Agent</el-dropdown-item>
    <el-dropdown-item command="group">修改分组</el-dropdown-item>
    <el-dropdown-item v-if="admin" command="revoke" divided>撤销凭证</el-dropdown-item>
    <el-dropdown-item v-if="admin" command="delete" style="color:var(--el-color-danger)">删除服务器</el-dropdown-item>
   </el-dropdown-menu></template>
  </el-dropdown>
  <span class="batch-count">已选 {{rows.length}} 台（当前页）</span>
 </div>
 <el-dialog v-model="visible" :title="names[action]" width="min(620px,94vw)" :close-on-click-modal="false" :close-on-press-escape="!running" :show-close="!running" append-to-body>
  <p class="batch-description">{{descriptions[action]}}</p>
  <el-input v-if="action==='group'&&!finished" v-model="group" aria-label="批量分组名称" placeholder="输入分组名称，留空清除分组" maxlength="64" :disabled="running"/>
  <p aria-live="polite">{{running?`正在处理 ${results.length} / ${targets.length} 台`:finished?summary:`将处理以下 ${targets.length} 台服务器，请确认：`}}</p>
  <ul class="batch-targets" v-if="!running&&!finished"><li v-for="row in targets" :key="row.id"><strong>{{row.name}}</strong><span v-if="skip(row)">将跳过：{{skip(row)}}</span></li></ul>
  <ul class="batch-targets" v-else><li v-for="row in results" :key="row.id"><strong>{{row.name}}</strong><span :class="{'batch-failure':row.state==='失败'}">{{row.state}} · {{row.message}}</span></li></ul>
  <template #footer><el-button :disabled="running" @click="visible=false">{{finished?'关闭':'取消'}}</el-button><el-button v-if="!finished" :type="action==='delete'||action==='revoke'?'danger':'primary'" :loading="running" @click="execute">{{running?'执行中…':'确认执行'}}</el-button></template>
 </el-dialog>
</template>
<style scoped>
.batch-controls{display:flex;align-items:center;gap:10px;flex-wrap:wrap}.batch-count{font-size:12px;color:var(--el-text-color-secondary)}.batch-description{line-height:1.7;color:var(--el-text-color-regular)}.batch-targets{list-style:none;padding:0;max-height:300px;overflow:auto}.batch-targets li{display:flex;flex-direction:column;gap:5px;padding:10px 0;border-bottom:1px solid var(--el-border-color-lighter);overflow-wrap:anywhere}.batch-targets span{font-size:12px;color:var(--el-text-color-secondary)}.batch-targets .batch-failure{color:var(--el-color-danger)}
</style>
