<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue';
import {loginLabel,loginUser,loginSource} from '../lib/loginEvents';
import CountryFlag from './CountryFlag.vue';

type Row=Record<string,any>;
const props=defineProps<{api:(path:string,method?:string,body?:any,signal?:AbortSignal)=>Promise<any>,refreshKey?:object}>();
const emit=defineEmits<{select:[event:Row],all:[]}>();
const events=ref<Row[]>([]),loading=ref(true),error=ref('');
const names=new Intl.DisplayNames(['zh-CN'],{type:'region'});
const country=(event:Row)=>!event.source_ip?'不可获取':event.country_code?(names.of(event.country_code)||event.country_name||event.country_code):(event.source_is_public===false?'内网 / 保留地址':'未知地区');
const source=loginSource;
const stamp=(value:string)=>new Date(value).toLocaleTimeString('zh-CN',{hour:'2-digit',minute:'2-digit'});
const fullStamp=(value:string)=>new Date(value).toLocaleString('zh-CN',{hour12:false});
let request:AbortController|undefined,destroyed=false;

async function load(){
 if(request)return;
 const current=new AbortController();request=current;error.value='';if(!events.value.length)loading.value=true;
 try{
  const result=await props.api('/events?page_size=5','GET',undefined,current.signal);
  if(!current.signal.aborted&&!destroyed)events.value=result.items;
 }catch(e){if(!current.signal.aborted&&!destroyed)error.value=(e as Error).message;}
 finally{if(request===current){request=undefined;if(!destroyed)loading.value=false;}}
}
watch(()=>props.refreshKey,()=>void load());
onMounted(()=>void load());
onUnmounted(()=>{destroyed=true;request?.abort();});
</script>

<template>
 <section class="panel recent-events" :aria-busy="loading">
  <div class="panel-heading"><h3>最近事件</h3><button class="text-action" type="button" @click="emit('all')">查看全部 →</button></div>
  <div v-if="loading" class="recent-events-skeleton" role="status" aria-label="正在加载最近事件"><div v-for="index in 5" :key="index"><span/><i/></div></div>
  <div v-else-if="!events.length" class="empty-state recent-events-empty" role="status"><b>{{error?'最近事件加载失败':'暂无登录事件'}}</b><button v-if="error" class="text-action" type="button" @click="load()">重试</button></div>
  <TransitionGroup v-else name="recent-event" tag="div" class="recent-events-list" appear>
   <button v-for="(event,index) in events" :key="event.id" type="button" class="recent-event-row" :style="{'--event-delay':index*35+'ms'}" :aria-label="`查看事件详情：${loginLabel(event)}，${loginUser(event)}，${source(event)}，${event.outcome==='success'?'登录成功':'认证失败'}，${fullStamp(event.occurred_at)}`" @click="emit('select',event)">
    <span :class="['alert-indicator',event.outcome==='success'?'success':'high']" aria-hidden="true"/>
    <span class="recent-event-copy">
     <span class="recent-event-primary"><b :title="loginLabel(event)+' · '+loginUser(event)">{{loginLabel(event)}}</b><span :title="source(event)">{{source(event)}}</span></span>
     <span class="recent-event-secondary"><span :title="event.server_name">{{event.server_name}}</span><span class="recent-event-country"><CountryFlag :code="event.country_code" :name="country(event)"/><span>{{country(event)}}</span></span></span>
    </span>
    <span :class="['status',event.outcome==='success'?'online':'failed']">{{event.outcome==='success'?'成功':'失败'}}</span><time :datetime="event.occurred_at" :title="fullStamp(event.occurred_at)">{{stamp(event.occurred_at)}}</time>
   </button>
  </TransitionGroup>
  <div v-if="error&&events.length" class="recent-events-error" role="status">更新失败 <button type="button" class="text-action" @click="load()">重试</button></div>
 </section>
</template>

<style scoped>
.recent-events{display:flex;flex-direction:column;min-width:0;overflow:hidden}.recent-events-list{position:relative}.recent-event-row{display:grid;grid-template-columns:8px minmax(0,1fr) auto 36px;align-items:center;gap:14px;width:100%;min-height:78px;padding:18px 24px;border:0;border-top:1px solid var(--line);background:var(--surface);color:var(--text-muted);text-align:left;transition:background-color .18s ease}.recent-event-row:hover,.recent-event-row:focus-visible{background:var(--surface-soft)}.recent-event-row:focus-visible{outline:2px solid #3b78ff;outline-offset:-3px}.recent-event-row .alert-indicator.success{border-color:#10b98a}.recent-event-copy{display:flex;flex-direction:column;gap:6px;min-width:0}.recent-event-primary,.recent-event-secondary{display:flex;align-items:center;gap:12px;min-width:0}.recent-event-primary b{max-width:125px;color:var(--text-strong);font-size:12px;font-weight:500;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.recent-event-primary>span{font-size:11px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.recent-event-secondary{font-size:10px;gap:10px}.recent-event-secondary>span:first-child{max-width:45%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.recent-event-country{display:inline-flex;align-items:center;gap:5px;min-width:0}.recent-event-country>span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.recent-event-country :deep(.country-flag){width:16px;height:12px;border-radius:2px}.recent-event-row time{font-size:10px;text-align:right;font-variant-numeric:tabular-nums}.recent-events-empty{display:flex;flex:1;min-height:200px;flex-direction:column;justify-content:center;align-items:center;gap:12px}.recent-events-skeleton>div{display:flex;min-height:78px;padding:23px 24px;border-top:1px solid var(--line);gap:20px;align-items:center;animation:recent-event-pulse 1s ease-in-out infinite alternate}.recent-events-skeleton span,.recent-events-skeleton i{height:6px;border-radius:4px;background:var(--line);display:block}.recent-events-skeleton span{width:56%}.recent-events-skeleton i{width:28px;margin-left:auto}.recent-events-error{padding:9px 24px;border-top:1px solid var(--line);color:var(--text-muted);font-size:10px}.recent-events-error button{font-size:10px}.recent-event-enter-active,.recent-event-leave-active{transition:opacity .25s ease,transform .3s ease;transition-delay:var(--event-delay,0ms)}.recent-event-enter-from,.recent-event-leave-to{opacity:0;transform:translateY(6px)}.recent-event-move{transition:transform .35s ease}.recent-event-leave-active{position:absolute;left:0;right:0;transition-delay:0ms}@keyframes recent-event-pulse{to{opacity:.4}}@media(max-width:700px){.recent-event-row{padding:16px;gap:10px}.recent-event-primary{gap:8px}.recent-event-primary b{max-width:78px}.recent-events-skeleton>div{padding-left:16px;padding-right:16px}}@media(prefers-reduced-motion:reduce){.recent-event-row,.recent-event-enter-active,.recent-event-leave-active,.recent-event-move{transition:none!important}.recent-events-skeleton>div{animation:none}}
</style>
