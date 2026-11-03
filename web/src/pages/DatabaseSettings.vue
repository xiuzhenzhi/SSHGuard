<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue';
import { ElButton, ElMessage, ElMessageBox } from 'element-plus';
type Row = Record<string, any>;
const props = defineProps<{api:(path:string,method?:string,body?:any,signal?:AbortSignal)=>Promise<any>}>();
const status = ref<Row>({}), loading = ref(true), saving = ref(false), starting = ref(false), error = ref('');
const form = reactive({events:30,alerts:90,audit:180});
const saved = ref({...form}), batchDays = ref(30);
const categories = [
  {key:'events' as const,name:'登录事件',description:'登录成功、认证失败及来源地址记录'},
  {key:'alerts' as const,name:'告警与投递记录',description:'过期告警与已送达通知；未送达或失败任务继续保留'},
  {key:'audit' as const,name:'操作审计',description:'登录、配置修改和服务器维护操作'},
];
const driver = computed(()=>({sqlite:'SQLite',postgres:'PostgreSQL',mysql:'MySQL'}[status.value.driver as string]||'—'));
const action = computed(()=>status.value.action==='vacuum_analyze'?'清理与分析':status.value.action==='optimize'?'优化数据库':'回收空间');
const actionDescription = computed(()=>status.value.driver==='postgres'?'对 SSHGuard 的表执行 VACUUM + ANALYZE，清理无效行并更新查询统计。空闲空间可供复用，文件大小不一定立即减少。':status.value.driver==='mysql'?'对 SSHGuard 的表执行 OPTIMIZE TABLE，整理表空间和索引并更新统计，可能需要临时空间及表锁。':'回收 SQLite 文件空闲空间（VACUUM），不会删除现有记录。');
const running = computed(()=>status.value.maintenance?.state==='running');
const dirty = computed(()=>categories.some(({key})=>form[key]!==saved.value[key]));
const valid = computed(()=>categories.every(({key})=>Number.isInteger(form[key])&&form[key]>=0&&form[key]<=3650));
function size(bytes:unknown) {if(typeof bytes!=='number')return '暂不可用';if(bytes<1024)return `${bytes} B`;const unit=Math.min(3,Math.floor(Math.log(bytes)/Math.log(1024)));return `${(bytes/1024**unit).toFixed(2)} ${['B','KB','MB','GB'][unit]}`;}
let timer:ReturnType<typeof setTimeout>|undefined, controller:AbortController|undefined, alive=true;
function poll(){clearTimeout(timer);if(alive&&running.value)timer=setTimeout(()=>void load(false),2000);}
async function load(reset=true){
  controller?.abort();controller=new AbortController();const signal=controller.signal;error.value='';
  try {
    const value=await props.api('/settings/database','GET',undefined,signal);
    if(!alive||signal.aborted)return;
    const wasRunning=running.value;
    status.value=value;
    if(wasRunning&&value.maintenance?.state==='completed')ElMessage.success('数据库维护完成');
    if(wasRunning&&value.maintenance?.state==='failed')ElMessage.error(value.maintenance.message||'数据库维护失败，请稍后重试');
    if(reset){Object.assign(form,value.retention);saved.value={...form};}
  }catch(e){if(alive&&!signal.aborted)error.value=(e as Error).message;}
  finally{if(alive&&!signal.aborted){loading.value=false;poll();}}
}
async function save(){
  if(!valid.value||!dirty.value||saving.value)return;
  const shortened=categories.some(({key})=>form[key]>0&&(saved.value[key]===0||form[key]<saved.value[key]));
  if(shortened){try{await ElMessageBox.confirm('缩短保留时间后，过期记录将在自动清理时永久删除。请确认需要保留的数据已备份。','确认保留策略',{type:'warning',confirmButtonText:'保存设置',cancelButtonText:'取消',closeOnClickModal:false,closeOnPressEscape:true});}catch{return;}}
  if(!alive||saving.value)return;
  const submitted={...form};
  saving.value=true;error.value='';
  try{await props.api('/settings/database','PATCH',submitted);if(!alive)return;saved.value=submitted;ElMessage.success('已保存，将在下一轮自动清理时生效');}
  catch(e){error.value=(e as Error).message;}finally{saving.value=false;}
}
async function reclaim(){
  if(running.value||starting.value)return;
  try{await ElMessageBox.confirm(actionDescription.value+'维护期间服务可能响应缓慢，请确认在空闲时段执行。','确认数据库维护',{type:'warning',confirmButtonText:action.value,cancelButtonText:'取消',closeOnClickModal:false,closeOnPressEscape:true});}catch{return;}
  if(!alive||running.value||starting.value)return;
  starting.value=true;error.value='';
  try{const task=await props.api('/settings/database/reclaim','POST',{confirm:true});if(!alive)return;status.value={...status.value,maintenance:task};poll();}
  catch(e){error.value=(e as Error).message;}finally{starting.value=false;}
}
defineExpose({refresh:load});
onMounted(()=>void load());onUnmounted(()=>{alive=false;clearTimeout(timer);controller?.abort();});
</script>

<template>
  <div class="database-settings">
    <p v-if="error" class="database-error" role="alert">{{error}} <button class="text-action" @click="load(false)">重试</button></p>
    <section class="panel database-card" :aria-busy="loading">
      <div class="database-summary"><div><strong>主数据库</strong><p>{{driver}} / {{status.local?'本机':'外部数据库'}}<span v-if="loading"> · 正在读取</span></p></div><strong>{{size(status.size)}}</strong></div>
      <div v-if="status.local" class="database-summary"><div><strong>本机数据库合计</strong><p>包含数据库文件、写入日志和共享内存文件</p></div><strong>{{size(status.size)}}</strong></div>
      <div class="database-maintenance"><p v-if="status.can_reclaim">{{actionDescription}}</p><p v-else>当前数据库暂不支持在线维护。</p><el-button v-if="status.can_reclaim" type="warning" :loading="starting||running" :class="{'is-maintaining':starting||running}" :disabled="loading||starting||running||saving" @click="reclaim">{{starting||running?'维护中…':action}}</el-button></div>
    </section>
    <section class="panel database-card">
      <header><div><h3>数据保留天数</h3><p>设置各类记录的保留时间，0 表示永久保留。自动清理每 10 分钟分批执行。</p></div></header>
      <form @submit.prevent="save">
        <div class="database-batch"><label>批量设置 <input v-model.number="batchDays" type="number" min="0" max="3650" step="1" aria-label="批量保留天数"/> 天</label><el-button :disabled="loading||saving||running||!Number.isInteger(batchDays)||batchDays<0||batchDays>3650" @click="Object.assign(form,{events:batchDays,alerts:batchDays,audit:batchDays})">应用到全部</el-button></div>
        <div class="database-table"><table><thead><tr><th>数据类型</th><th>说明</th><th>保留天数</th></tr></thead><tbody><tr v-for="item in categories" :key="item.key"><td>{{item.name}}</td><td>{{item.description}}</td><td><input v-model.number="form[item.key]" type="number" min="0" max="3650" step="1" required :disabled="loading||saving||running" :aria-label="item.name+'保留天数'"/></td></tr></tbody></table></div>
        <footer><p>保存后即时启用新策略；过期数据分批删除，不会立即清空。</p><el-button type="primary" native-type="submit" :loading="saving" :disabled="loading||running||!dirty||!valid">保存设置</el-button></footer>
      </form>
    </section>
  </div>
</template>
<style scoped>
.database-maintenance{justify-content:space-between}.database-maintenance .is-maintaining{background:var(--surface-soft);border-color:var(--line);color:var(--text-muted);opacity:.65}.database-settings{display:grid;gap:20px}.database-card{padding:24px}.database-card header,.database-summary,.database-maintenance,.database-card footer{display:flex;align-items:center;justify-content:space-between;gap:18px}.database-card header{padding:0;height:auto;position:static;box-shadow:none;border:0;background:transparent}.database-card h3{margin:0 0 8px}.database-card p{color:var(--text-muted);font-size:13px;line-height:1.65;margin:6px 0}.database-summary{padding:20px 0;border-bottom:1px solid var(--line)}.database-maintenance,.database-card footer{padding-top:18px}.database-batch{display:flex;align-items:center;gap:12px;margin:20px 0}.database-card input{width:110px;padding:8px 10px;border:1px solid var(--line);border-radius:8px;background:var(--surface);color:var(--text-strong);font:inherit}.database-card input:focus{outline:2px solid var(--el-color-primary);outline-offset:2px}.database-table{overflow-x:auto}.database-table table{width:100%;border-collapse:collapse;text-align:left;font-size:13px}.database-table th{background:var(--surface-soft);color:var(--text-muted);font-weight:500}.database-table th,.database-table td{padding:14px 12px;border-bottom:1px solid var(--line)}.database-error{color:var(--el-color-danger)!important}.database-card footer p{max-width:75%}@media(max-width:700px){.database-card{padding:16px}.database-maintenance,.database-card footer{align-items:flex-start;flex-direction:column}.database-card footer p{max-width:100%}.database-table table{min-width:0}.database-table thead{display:none}.database-table tr{display:grid;grid-template-columns:1fr auto;align-items:center;padding:12px 0;border-bottom:1px solid var(--line)}.database-table td{border:0;padding:6px 0}.database-table td:first-child{grid-column:1;grid-row:1}.database-table td:last-child{grid-column:2;grid-row:1}.database-table td:nth-child(2){grid-column:1/-1;grid-row:2;color:var(--text-muted)}.database-table input{width:86px}.database-batch{flex-wrap:wrap}}
</style>
