<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue';
import { ElMessage, ElMessageBox, ElIcon, ElForm, ElFormItem, ElInput, ElButton, ElAlert, ElSelect, ElOption, ElTable, ElTableColumn, ElDialog, ElDrawer } from 'element-plus';
import 'element-plus/es/components/icon/style/css';
import 'element-plus/es/components/form/style/css';
import 'element-plus/es/components/form-item/style/css';
import 'element-plus/es/components/input/style/css';
import 'element-plus/es/components/button/style/css';
import 'element-plus/es/components/alert/style/css';
import 'element-plus/es/components/select/style/css';
import 'element-plus/es/components/option/style/css';
import 'element-plus/es/components/table/style/css';
import 'element-plus/es/components/table-column/style/css';
import 'element-plus/es/components/dialog/style/css';
import 'element-plus/es/components/drawer/style/css';
import 'element-plus/es/components/message/style/css';
import 'element-plus/es/components/message-box/style/css';
import { Monitor, Grid, Connection, Bell, Message, Filter, CircleCheck, Key, Promotion, DocumentChecked, User, Lock, Setting, MapLocation, Plus, Refresh, Search, ArrowRight, Moon, Sunny, Expand, Close } from '@element-plus/icons-vue';
import LoginIntro from './components/LoginIntro.vue';
import {loginLabel,loginUser,loginSource,consoleHint} from './lib/loginEvents';
import ConsoleNavigation from './components/ConsoleNavigation.vue';
const loadOverview = () => import('./pages/Overview.vue');
const Overview = lazyPage(loadOverview);
import CountryFlag from './components/CountryFlag.vue';
const loadAlertCenter = () => import('./pages/AlertCenter.vue');
const AlertCenter = lazyPage(loadAlertCenter);
const loadServerList = () => import('./pages/ServerList.vue');
const ServerList = lazyPage(loadServerList);
const loadAllowlist = () => import('./pages/Allowlist.vue');
const Allowlist = lazyPage(loadAllowlist);
const loadLoginProtection = () => import('./pages/LoginProtection.vue');
const LoginProtection = lazyPage(loadLoginProtection);
const loadGeoIPSettings = () => import('./pages/GeoIPSettings.vue');
const GeoIPSettings = lazyPage(loadGeoIPSettings);
import CaptchaWidget from './components/CaptchaWidget.vue';
const loadDatabaseSettings = () => import('./pages/DatabaseSettings.vue');
const DatabaseSettings = lazyPage(loadDatabaseSettings);
const loadSetupWizard = () => import('./pages/SetupWizard.vue');
const SetupWizard = lazyPage(loadSetupWizard);
import { lazyPage } from './lib/lazyPage';
import { createResponseCache } from './lib/responseCache';
import { requestJSON, HTTPError } from './lib/api';
import { copyText } from './lib/clipboard';
import { isDark, toggleTheme, observeTheme } from './lib/theme';
import { pageFromPath, pathForPage } from './lib/routes';
const stopTheme=observeTheme();
onUnmounted(stopTheme);
const setupRequired = ref(false), bootError = ref('');
const mobileViewport = window.matchMedia('(max-width: 700px)');
const isMobile = ref(mobileViewport.matches), navigationOpen = ref(false);
function updateViewport(event: MediaQueryListEvent) {isMobile.value=event.matches;navigationOpen.value=false;}
mobileViewport.addEventListener('change',updateViewport);
onUnmounted(()=>mobileViewport.removeEventListener('change',updateViewport));
const protectionPage=ref<InstanceType<typeof LoginProtection>>();
const databasePage=ref<InstanceType<typeof DatabaseSettings>>();
const geoIPPage=ref<InstanceType<typeof GeoIPSettings>>();
type Row = Record<string, any>;
const user = ref<Row | null>(null), booting = ref(true), busy = ref(false), error = ref('');
const active = ref<string>(pageFromPath(location.pathname)), rows = ref<Row[]>([]), summary = ref<Row>({}), page = ref(1), more = ref(false);
const serverPageSizes = [10, 20, 50, 100];
const serverPageSize = ref(20);
const overview = ref<Row>({});
const trendRange=ref('24h');
const management = computed(() => ['alerts','channels','rules','notifications'].includes(active.value));
const liveState = ref('连接中');
const refreshing = ref(false);
const eventType = ref('');
const search = ref(''), outcome = ref(''), username = ref(''), ip = ref(''), serverID = ref('');
const login = reactive({username: 'admin', password: ''});
const protection=ref<Row>({provider:'loading',site_key:''}),captchaAnswer=ref<Row>({}),captchaWidget=ref<InstanceType<typeof CaptchaWidget>>(),protectionError=ref(''),loginError=ref('');
const captchaReady=computed(()=>protection.value.provider==='none'||(protection.value.provider==='geetest'?!!captchaAnswer.value.lot_number:!!captchaAnswer.value.token));
async function loadProtection(){protectionError.value='';try{protection.value=await api('/auth/protection')}catch(e){protection.value={provider:'loading',site_key:''};protectionError.value=(e as Error).message}}
watch(()=>!!user.value,value=>{if(!value&&!setupRequired.value)void loadProtection()});
const serverDialog = ref(false), userDialog = ref(false), commandDialog = ref(false), detail = ref<Row | null>(null);
const passwordDialog = ref(false), updating = ref(false);
const passwords = reactive({current_password: '', new_password: ''});
const commands = ref<Record<string,string>>({}), platform = ref('linux'), expires = ref('');
const commandEndpoint = ref(''), commandInsecure = ref(false);
const command = computed(() => commands.value[platform.value] || '');
const server = reactive({id: '', name: '', group_name: '', notes: ''});
const commandServerID=ref(''),commandInterval=ref(5),generatedCommandInterval=ref(0);
const commandGenerating=ref(false),commandError=ref('');
const validCommandInterval=computed(()=>Number.isInteger(commandInterval.value)&&commandInterval.value>=1&&commandInterval.value<=600);
const commandReady=computed(()=>!!command.value&&(updating.value||(validCommandInterval.value&&!commandGenerating.value&&commandInterval.value===generatedCommandInterval.value&&!commandError.value)));
let commandGeneration=0,commandTimer:ReturnType<typeof setTimeout>|undefined,enrollmentRequest:Promise<void>|undefined;
function scheduleEnrollment(){
  clearTimeout(commandTimer);commandError.value='';
  if(commandDialog.value&&!updating.value&&validCommandInterval.value&&commandInterval.value!==generatedCommandInterval.value)commandTimer=setTimeout(()=>void refreshEnrollment(),350);
}
async function refreshEnrollment(){
  if(enrollmentRequest)return enrollmentRequest;
  commandGenerating.value=true;commandError.value='';
  enrollmentRequest=(async()=>{
    while(commandDialog.value&&!updating.value&&validCommandInterval.value&&commandInterval.value!==generatedCommandInterval.value){
      const generation=commandGeneration,id=commandServerID.value,interval=commandInterval.value;
      commands.value={};generatedCommandInterval.value=0;
      try{
        const result=await api('/servers/'+id+'/enrollment','POST',{report_interval_seconds:interval});
        if(generation!==commandGeneration)continue;
        clearTimeout(commandTimer);commands.value=result.commands;expires.value=result.expires_at;commandEndpoint.value=result.endpoint;commandInsecure.value=result.insecure_http;generatedCommandInterval.value=interval;
      }catch(e){
        if(generation===commandGeneration&&interval===commandInterval.value){commandError.value=(e as Error).message;break;}
      }
    }
  })().finally(()=>{commandGenerating.value=false;enrollmentRequest=undefined;});
  return enrollmentRequest;
}
watch(commandInterval,scheduleEnrollment);
watch(commandDialog,open=>{if(!open){commandGeneration++;clearTimeout(commandTimer);commands.value={};commandError.value='';}},{flush:'sync'});
onUnmounted(()=>{commandDialog.value=false;commandGeneration++;clearTimeout(commandTimer);});
const newUser = reactive({username: '', password: '', role: 'viewer'});
const admin = computed(() => user.value?.role === 'admin');
const writer = computed(() => ['admin', 'operator'].includes(user.value?.role));
const nav = computed(() => [
  {id: 'dashboard', title: '安全总览', icon: Grid}, {id: 'servers', title: '服务器管理', icon: Monitor},
  {id: 'events', title: '登录事件', icon: Connection}, {id: 'alerts', title: '告警中心', icon: Bell},
  ...(admin.value ? [{id:'channels',title:'通知渠道',icon:Message},{id:'rules',title:'检测规则',icon:Filter},{id:'ip-allowlist',title:'IP 白名单',icon:CircleCheck},{id:'geoip',title:'地理位置',icon:MapLocation},{id:'database',title:'数据库',icon:DocumentChecked},{id:'login-protection',title:'登录防护',icon:Key},{id:'notifications',title:'投递记录',icon:Promotion},{id: 'audit-logs', title: '操作审计', icon: DocumentChecked}, {id: 'users', title: '用户与权限', icon: User}] : [])
]);
const title = computed(() => nav.value.find(x => x.id === active.value)?.title || '安全总览');
const roleNames:Row={admin:'管理员',operator:'操作员',viewer:'只读用户'};
const auditNames:Row={'auth.login':'登录','auth.logout':'退出登录','auth.password.change':'修改密码','user.create':'创建用户','server.create':'添加服务器','server.update':'编辑服务器','server.reporting.update':'修改上报间隔','agent.enrolled':'Agent 注册','agent.enrollment.create':'生成安装命令','agent.revoke':'撤销 Agent 凭证','agent.update.request':'请求更新 Agent','agent.update.success':'Agent 更新成功','agent.update.failed':'Agent 更新失败','channel.create':'添加通知渠道','channel.update':'编辑通知渠道','channel.test':'测试通知渠道','notification.retry':'重试通知投递','rule.save':'保存检测规则','alert.acknowledge':'确认告警','auth.ip.block':'封禁登录来源','auth.ip.unblock':'解除登录封禁','auth.protection.update':'更新登录防护','allowlist.create':'添加 IP 白名单','allowlist.delete':'移除 IP 白名单'};
const countryNames=new Intl.DisplayNames(['zh-CN'],{type:'region'});
const countryName=(row:Row)=>!row.source_ip?'不可获取':row.country_code?(countryNames.of(row.country_code)||row.country_name||row.country_code):(row.source_is_public===false?'内网 / 保留地址':'未知');
const sourceAddress=loginSource;
Object.assign(auditNames,{'geoip.settings.update':'更新地理位置设置','geoip.database.update':'更新 GeoIP 数据库','alert.acknowledge.batch':'批量确认告警'});
Object.assign(auditNames,{'server.delete':'删除服务器','database.retention.update':'修改数据保留策略','database.maintenance.start':'开始数据库维护','database.maintenance.completed':'数据库维护完成','database.maintenance.failed':'数据库维护失败'});
const time = (s: any) => s ? new Date(s).toLocaleString('zh-CN', {hour12: false}) : '尚无记录';
let generation = 0;
let controller: AbortController | undefined;
let loading = false;
const responses = createResponseCache();
let cancelWarm = () => {};
function warmPages() {
  cancelWarm();
  let cancelled = false, cancelScheduled = () => {};
  cancelWarm = () => { cancelled = true; cancelScheduled(); };
  const connection = (navigator as Navigator & {connection?: {saveData?: boolean}}).connection;
  if (connection?.saveData) return;
  const loaders = [loadServerList, loadOverview, loadAlertCenter, ...(admin.value ? [loadAllowlist, loadLoginProtection, loadGeoIPSettings, loadDatabaseSettings] : []), () => import('./components/LoginTrend.vue')];
  const schedule = () => {
    if (cancelled || !user.value || !loaders.length) return;
    const next = () => { if (cancelled || !user.value) return; void loaders.shift()!().catch(() => {}).finally(schedule); };
    if ('requestIdleCallback' in window) {
      const id = window.requestIdleCallback(next, {timeout:3000}); cancelScheduled = () => window.cancelIdleCallback(id);
    } else { const id = setTimeout(next, 800); cancelScheduled = () => clearTimeout(id); }
  };
  schedule();
}
function clearSession() {
  responses.clear(); cancelWarm();
  navigationOpen.value = false;
  stopLive(); overview.value = {};
  controller?.abort(); generation++; loading = false;
  user.value = null; rows.value = []; summary.value = {}; error.value = '';
  loginError.value='';active.value = 'dashboard'; page.value = 1; more.value = false;
  commands.value = {}; commandDialog.value = false; detail.value = null;
  serverDialog.value = false; userDialog.value = false; passwordDialog.value = false;
  passwords.current_password = ''; passwords.new_password = ''; newUser.password = ''; login.password = '';
  eventType.value = ''; search.value = ''; outcome.value = ''; username.value = ''; ip.value = ''; serverID.value = '';
}
async function api(path: string, method = 'GET', body?: any, signal?: AbortSignal) {
  const requestUser=user.value;
  if (method !== 'GET') responses.clear();
  const epoch = responses.epoch;
  try {
    const data = await requestJSON(path, method, body, signal);
    if (method !== 'GET') responses.clear();
    if (method === 'GET' && user.value === requestUser && !signal?.aborted) responses.set(path, data, epoch);
    return data;
  }
  catch (e) { if (e instanceof HTTPError && e.status === 401 && user.value === requestUser) clearSession(); throw e; }
}
async function cachedAPI(path: string, method = 'GET', body?: any, signal?: AbortSignal) {
  const cached = method === 'GET' ? responses.get(path) : undefined;
  return cached === undefined ? api(path, method, body, signal) : cached;
}
function listPath() {
  const params = new URLSearchParams({page: String(page.value), page_size: active.value === 'rules' ? '100' : active.value === 'servers' ? String(serverPageSize.value) : '20'});
  if (active.value === 'servers' && search.value) params.set('q',search.value);
  if (active.value === 'events') {if(eventType.value)params.set('event_type',eventType.value);if (outcome.value) params.set('outcome',outcome.value);if (username.value) params.set('username',username.value);if (ip.value) params.set('source_ip',ip.value);if (serverID.value) params.set('server_id',serverID.value);}
  return `/${active.value}?${params}`;
}
function restoreCachedPage() {
  const cached = responses.get(listPath(), 120000);
  rows.value = cached?.items || []; more.value = cached?.has_more || false;
  if (active.value === 'dashboard') overview.value = responses.get('/dashboard/overview?range='+trendRange.value, 120000) || {};
}
async function perform(fn: () => Promise<void>) {busy.value = true; try {await fn();} catch(e) {ElMessage.error((e as Error).message);} finally {busy.value = false;}}
async function signIn() {
  if(busy.value || !captchaReady.value || !login.username.trim() || !login.password)return;
  loginError.value='';await perform(async () => {try{user.value = await api('/auth/login', 'POST', {...login,captcha:captchaAnswer.value});login.password='';restoreRoute();await nextTick();await load();startLive();warmPages();}catch(e){loginError.value=(e as Error).message;throw e;}finally{captchaAnswer.value={};captchaWidget.value?.reset();if(!user.value)await loadProtection();}});
}
async function finishSetup(credentials: {username:string;password:string}) {
  setupRequired.value = false; login.username = credentials.username;
  await perform(async () => {
    try {
      user.value = await api('/auth/login', 'POST', credentials);
      await navigate('servers'); startLive(); warmPages();
      ElMessage.success('主控已就绪，添加第一台服务器开始采集');
    } finally { credentials.password = ''; }
  });
}
async function signOut() {controller?.abort(); generation++; await perform(async () => {await api('/auth/logout','POST');clearSession();});}
async function load(preferCache = false) {
  const read = preferCache ? cachedAPI : api;
  if(user.value)writeRoute();
  controller?.abort(); controller = new AbortController(); const signal = controller.signal;
  const request = ++generation; error.value = ''; loading = true;
  try {
    if (active.value === 'dashboard') {
      await Promise.all([
        read('/dashboard/summary','GET',undefined,signal).then(value => {if (request === generation) summary.value=value;}),
        read('/dashboard/overview?range='+trendRange.value,'GET',undefined,signal).then(value => {if (request === generation) overview.value=value;}),
      ]);
      if (request === generation) rows.value=[];
      return;
    }
    if (!summary.value.version) void cachedAPI('/dashboard/summary','GET',undefined,signal).then(value => {if (request === generation) summary.value = value;}).catch(() => {});
    if(active.value==='login-protection'){rows.value=[];if(!preferCache)await protectionPage.value?.refresh();return;}
    if(active.value==='database'){rows.value=[];if(!preferCache)await databasePage.value?.refresh();return;}
    if(active.value==='geoip'){rows.value=[];if(!preferCache)await geoIPPage.value?.refresh();return;}
    const result = await read(listPath(), 'GET', undefined, signal);
    if (request !== generation) return; rows.value = result.items; more.value = result.has_more;
  } catch (e) {if (request === generation && !signal.aborted) error.value = (e as Error).message;}
  finally {if (request === generation) loading = false;}
}
function writeRoute(mode:'push'|'replace'='replace') {
  const params=new URLSearchParams();
  if(active.value==='dashboard') {if(trendRange.value!=='24h')params.set('range',trendRange.value);}
  else if(page.value>1)params.set('page',String(page.value));
  if(active.value==='servers'&&search.value)params.set('q',search.value);
  if(active.value==='servers'&&serverPageSize.value!==20)params.set('page_size',String(serverPageSize.value));
  if(active.value==='events')for(const [key,value] of Object.entries({event_type:eventType.value,username:username.value,source_ip:ip.value,server_id:serverID.value,outcome:outcome.value}))if(value)params.set(key,value);
  const query=params.toString(),path=pathForPage(active.value)+(query?'?'+query:'');
  if(path!==location.pathname+location.search)window.history[mode==='push'?'pushState':'replaceState'](null,'',path);
}
function restoreRoute() {
  const id=pageFromPath(location.pathname),params=new URLSearchParams(location.search);
  active.value=!user.value||nav.value.some(item=>item.id===id)?id:'dashboard';
  const number=Number(params.get('page')||1);
  page.value=Number.isSafeInteger(number)&&number>0?number:1;
  const requestedSize=Number(params.get('page_size')||20);
  if(active.value==='servers')serverPageSize.value=serverPageSizes.includes(requestedSize)?requestedSize:20;
  search.value=active.value==='servers'?params.get('q')||'':'';
  username.value=active.value==='events'?params.get('username')||'':'';
  ip.value=active.value==='events'?params.get('source_ip')||'':'';
  serverID.value=active.value==='events'?params.get('server_id')||'':'';
  outcome.value=active.value==='events'?params.get('outcome')||'':'';
  eventType.value=active.value==='events'?params.get('event_type')||'':'';
  const range=params.get('range')||'24h';
  trendRange.value=active.value==='dashboard'&&['24h','7d','14d','30d'].includes(range)?range:'24h';
  restoreCachedPage();detail.value=null;
  if(user.value)writeRoute();
}
async function navigate(id:string) {
  navigationOpen.value=false;
  clearTimeout(refreshTimer);refreshTimer=undefined;
  controller?.abort();generation++;
  active.value=nav.value.some(item=>item.id===id)?id:'dashboard';page.value=1;restoreCachedPage();detail.value=null;
  writeRoute('push');await nextTick();if(user.value)await load(true);
}
async function onRouteChange(){navigationOpen.value=false;clearTimeout(refreshTimer);refreshTimer=undefined;controller?.abort();generation++;restoreRoute();await nextTick();if(user.value)await load(true);}
window.addEventListener('popstate',onRouteChange);
async function refreshPage() {if(refreshing.value)return;responses.clear();refreshing.value=true;try{await load();}finally{refreshing.value=false;}}
function filter() {page.value = 1;void load();}
function changeServerPageSize() {page.value=1;rows.value=[];more.value=false;void load();}
function editServer(row?: Row) {Object.assign(server, {id: row?.id || '',name: row?.name || '',group_name: row?.group_name || '',notes: row?.notes || ''});serverDialog.value = true;}
async function saveServer() {if(busy.value)return;await perform(async () => {const created = !server.id; const result = await api('/servers' + (server.id ? '/' + server.id : ''),server.id ? 'PATCH' : 'POST',server);serverDialog.value = false;ElMessage.success('服务器已保存');await load();if(created && admin.value) await enrollment({...server,id:result.id});});}
async function enrollment(row: Row) {
  commandGeneration++;clearTimeout(commandTimer);commands.value={};commandError.value='';expires.value='';commandEndpoint.value='';commandInsecure.value=false;
  commandServerID.value=row.id;commandInterval.value=row.report_interval_seconds||5;generatedCommandInterval.value=0;updating.value=false;platform.value=/windows/i.test(row.distribution)?'windows':'linux';commandDialog.value=true;
  await refreshEnrollment();
}
async function updateAgent(row: Row) {commandGeneration++;clearTimeout(commandTimer);commands.value={};commandError.value='';updating.value=true;await perform(async () => {const result = await api('/agent/update-commands'); commands.value = result.commands;commandEndpoint.value = result.endpoint;commandInsecure.value = result.insecure_http;platform.value = /windows/i.test(row.distribution) ? 'windows' : 'linux'; commandDialog.value = true;});}
async function changePassword() {await perform(async () => {await api('/auth/password','POST',passwords); clearSession(); ElMessage.success('密码已修改，所有旧会话已失效，请重新登录');});}
async function revoke(row: Row) {try {await ElMessageBox.confirm(`撤销 ${row.name} 的 Agent 凭证后将拒绝其后续上报。`, '撤销凭证', {type:'warning',confirmButtonText:'撤销',cancelButtonText:'取消',closeOnClickModal:false,closeOnPressEscape:true});}catch{return;}await perform(async () => {await api(`/servers/${row.id}/revoke`,'POST',{});await load();});}
async function deleteServer(row: Row) {
  if(busy.value)return;
  try {await ElMessageBox.confirm(`确定删除服务器“${row.name}”？该服务器的 登录事件、告警和通知投递记录将永久删除，安装及 Agent 凭证立即失效。操作审计保留。此操作无法撤销；目标机器上的 Agent 需另行卸载。`, '删除服务器', {type:'warning',confirmButtonText:'确认删除',cancelButtonText:'取消',confirmButtonClass:'el-button--danger',closeOnClickModal:false,closeOnPressEscape:true});}catch{return;}
  await perform(async () => {
    await api(`/servers/${row.id}`,'DELETE');
    ElMessage.success('服务器已删除');
    if(active.value==='servers'){
      rows.value=rows.value.filter(item=>item.id!==row.id);
      if(!rows.value.length&&page.value>1)page.value--;
    }
    summary.value={};
    await load();
  });
}
async function saveUser() {await perform(async () => {await api('/users','POST',newUser);userDialog.value = false;newUser.password = '';await load();});}
async function copyCommand() {if(!commandReady.value)return;try {await copyText(command.value);ElMessage.success('命令已复制');}catch(e){ElMessage.error((e as Error).message||'请手动复制命令');}}
function history(row: Row) {serverID.value = row.id;navigate('events');}
function showAllEvents(){eventType.value='';username.value='';ip.value='';serverID.value='';outcome.value='';void navigate('events');}

let socket: WebSocket | null = null;
let reconnect: ReturnType<typeof setTimeout> | undefined;
let refreshTimer: ReturnType<typeof setTimeout> | undefined;
let retryDelay = 1000;
function stopLive() {
  clearTimeout(reconnect);clearTimeout(refreshTimer);
  reconnect=undefined;refreshTimer=undefined;
  if(socket){socket.onclose=null;socket.close();socket=null;}
  liveState.value='连接中';
}
function scheduleRefresh() {
  responses.clear();
  if(refreshTimer || !user.value || document.hidden || ['login-protection','geoip','database'].includes(active.value))return;
  refreshTimer=setTimeout(()=>{refreshTimer=undefined;if(!user.value)return;if(loading || busy.value){scheduleRefresh();return;}void load();},500);
}
function startLive() {
  stopLive();if(!user.value)return;
  const current = new WebSocket((location.protocol==='https:'?'wss://':'ws://')+location.host+'/api/v1/live');
  socket=current;
  current.onopen=()=>{if(socket!==current)return;liveState.value='实时连接';retryDelay=1000;scheduleRefresh();};
  current.onmessage=event=>{if(socket!==current)return;try{if(JSON.parse(event.data).type==='refresh')scheduleRefresh();}catch{}};
  current.onclose=async event=>{
    if(socket!==current || !user.value)return;
    liveState.value='定时同步';
    if(event.code===1008){
      // A proxy or older server can use 1008 for a transport failure. Only a
      // confirmed HTTP 401 should end the browser session (handled by api).
      try{await api('/auth/me');}catch{}
      if(socket!==current || !user.value)return;
    }
    socket=null;reconnect=setTimeout(startLive,retryDelay+Math.random()*1000);retryDelay=Math.min(30000,retryDelay*2);
  };
}
function onVisible(){if(!document.hidden && user.value)scheduleRefresh();}
document.addEventListener('visibilitychange',onVisible);

let timer: ReturnType<typeof setInterval>;
async function bootstrap() {
  booting.value = true; bootError.value = '';
  try {
    setupRequired.value = (await api('/setup/status')).required;
    if (!setupRequired.value) {
      try { user.value = await api('/auth/me'); }
      catch (e) { if (!(e instanceof HTTPError && e.status === 401)) throw e; }
      if (user.value) restoreRoute();
    }
  } catch (e) { bootError.value = (e as Error).message; }
  finally { booting.value = false; }
  if (bootError.value || setupRequired.value) return;
  if (user.value) { await nextTick(); void load(); startLive(); warmPages(); }
  else void loadProtection();
}
onMounted(() => {
  void bootstrap();
  timer = setInterval(() => {if (user.value && !document.hidden && !busy.value && !loading && ['dashboard','servers','events','alerts','notifications'].includes(active.value)) void load();},30000);
});
onUnmounted(() => {stopLive();cancelWarm();responses.clear();clearInterval(timer); controller?.abort();document.removeEventListener('visibilitychange',onVisible);window.removeEventListener('popstate',onRouteChange);});
</script>

<template>
  <div v-if="!user" class="theme-toggle-fixed"><el-button circle :icon="isDark?Sunny:Moon" :aria-label="isDark?'切换浅色模式':'切换深色模式'" @click="toggleTheme"/></div>
  <div v-if="booting" class="loading">正在连接 SSHGuard…</div>
  <div v-else-if="bootError" class="loading" role="alert"><p>{{ bootError }}</p><el-button @click="bootstrap">重新连接</el-button></div>
  <SetupWizard v-else-if="setupRequired" @complete="finishSetup" />
  <main v-else-if="!user" class="login-screen">
    <LoginIntro/>
    <section class="login-form"><div class="eyebrow">欢迎回来</div><h2>登录管理控制台</h2><p class="login-subtitle">欢迎回来，继续守护你的服务器。</p><el-form label-position="top" @submit.prevent="signIn"><el-form-item label="用户名"><el-input v-model="login.username" autocomplete="username" size="large" /></el-form-item><el-form-item label="密码"><el-input v-model="login.password" type="password" show-password autocomplete="current-password" size="large" /></el-form-item><CaptchaWidget v-if="['turnstile','recaptcha','geetest'].includes(protection.provider)" :key="protection.provider+protection.site_key" ref="captchaWidget" :provider="protection.provider" :site-key="protection.site_key" @answer="value=>captchaAnswer=value"/><p v-if="loginError" class="captcha-error" role="alert">{{loginError}}</p><p v-if="protectionError" class="captcha-error" role="alert">{{protectionError}} <button type="button" class="text-action" @click="loadProtection">重试</button></p><el-button type="primary" size="large" native-type="submit" :disabled="!captchaReady || !login.username.trim() || !login.password" :loading="busy">安全登录 <el-icon><ArrowRight /></el-icon></el-button></el-form><p class="login-footnote"><el-icon><Lock/></el-icon>SSHGuard · 自托管登录审计平台</p></section>
  </main>
  <div v-else class="shell">
    <aside v-if="!isMobile"><ConsoleNavigation :items="nav" :active="active" :version="summary.version" @navigate="navigate"/></aside>
    <el-drawer v-else id="mobile-navigation" v-model="navigationOpen" class="mobile-navigation" direction="ltr" size="min(292px, 86vw)" title="导航菜单" :with-header="false" append-to-body destroy-on-close :close-on-click-modal="false" :close-on-press-escape="true">
      <ConsoleNavigation :items="nav" :active="active" :version="summary.version" @navigate="navigate">
        <button class="navigation-toggle" type="button" aria-label="关闭导航菜单" @click="navigationOpen=false"><el-icon><Close/></el-icon></button>
      </ConsoleNavigation>
    </el-drawer>
    <div class="main"><header class="console-header"><div class="header-location"><button v-if="isMobile" class="navigation-toggle" type="button" aria-label="打开导航菜单" aria-controls="mobile-navigation" :aria-expanded="navigationOpen" @click="navigationOpen=true"><el-icon><Expand/></el-icon></button><div class="breadcrumb"><span class="breadcrumb-root">控制台 /</span><span class="current-page">{{ title }}</span></div></div><div class="account"><b>{{ user.role==='admin'?'Admin':user.username }}</b><span class="sync-status" :class="{connected:liveState==='实时连接'}"><i/>{{liveState}}</span><el-button class="header-tool refresh-tool" :class="{'is-refreshing':refreshing}" :icon="Refresh" :disabled="refreshing" title="刷新当前页面" aria-label="刷新当前页面" @click="refreshPage"/><el-button class="header-tool theme-toggle" :icon="isDark?Sunny:Moon" :title="isDark?'切换浅色模式':'切换深色模式'" :aria-label="isDark?'切换浅色模式':'切换深色模式'" @click="toggleTheme"/><el-button class="header-tool settings-tool" :icon="Setting" title="修改密码" aria-label="修改密码" @click="passwordDialog=true"/><el-button class="header-tool logout-tool" title="退出登录" aria-label="退出登录" :disabled="busy" @click="signOut"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M9 4H5a1 1 0 0 0-1 1v14a1 1 0 0 0 1 1h4"/><path class="logout-arrow" d="M13 8l4 4-4 4m-5-4h9"/></svg></el-button></div></header>
    <section class="content"><div v-if="active!=='dashboard'" class="page-title"><h1>{{ title }}</h1><div class="actions"><el-button v-if="active === 'servers' && writer" type="primary" :icon="Plus" @click="editServer()">添加服务器</el-button><el-button v-if="active === 'users' && admin" type="primary" :icon="Plus" @click="userDialog = true">创建用户</el-button></div></div>
    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" class="notice" />
    <Overview :api="cachedAPI" v-if="active === 'dashboard'" :summary="summary" :overview="overview" :admin="admin" :range="trendRange" @range="(value:string)=>{trendRange=value;void load()}" @navigate="navigate" @show-event="event=>detail=event" @all-events="showAllEvents"/>
    <LoginProtection v-else-if="active==='login-protection'" ref="protectionPage" :api="cachedAPI"/>
    <GeoIPSettings v-else-if="active==='geoip'" ref="geoIPPage" :api="cachedAPI"/>
    <DatabaseSettings v-else-if="active==='database'" ref="databasePage" :api="api"/>
    <Allowlist v-else-if="active==='ip-allowlist'" :rows="rows" :api="cachedAPI" @refresh="load"/>
    <AlertCenter v-else-if="management" :key="active" :mode="active" :rows="rows" :admin="admin" :writer="writer" :api="cachedAPI" @refresh="load"/>
    <div v-else class="panel">
    <div v-if="active === 'events'" class="event-filters"><el-select v-model="eventType" placeholder="全部登录类型" clearable><el-option v-for="kind in ['ssh','rdp','vnc','console']" :key="kind" :label="loginLabel({event_type:kind+'.login'})+' · 成功'" :value="kind+'.login'"/><el-option v-for="kind in ['ssh','rdp','vnc','console']" :key="kind+'-failed'" :label="loginLabel({event_type:kind+'.login'})+' · 失败'" :value="kind+'.login_failed'"/></el-select><el-input v-model="username" placeholder="用户名" clearable/><el-input v-model="ip" placeholder="来源 IP" clearable/><el-input v-model="serverID" placeholder="服务器 ID" clearable/><el-select v-model="outcome" placeholder="全部结果" clearable><el-option label="成功" value="success"/><el-option label="失败" value="failure"/></el-select><el-button type="primary" @click="filter">筛选</el-button></div>
    <p v-if="active==='events'" class="event-console-note">“远程 VNC 登录”表示{{consoleHint}}</p>
    <ServerList v-if="active==='servers'" :rows="rows" :admin="admin" :writer="writer" :busy="busy" :selection-key="page+':'+search+':'+serverPageSize" @history="history" @edit="editServer" @enroll="enrollment" @update="updateAgent" @revoke="revoke" @remove="deleteServer" @changed="responses.clear();load()"><template #filters><div class="filters"><el-input v-model="search" placeholder="搜索名称、主机名或分组" :prefix-icon="Search" clearable @keyup.enter="filter" @clear="filter"/><el-button @click="filter">查询</el-button></div></template></ServerList>
    <el-table v-else-if="active === 'events'" :data="rows" empty-text="暂无 登录事件，请检查 Agent 采集状态并发起一次登录。" @row-click="(row: Row) => detail = row">
      <el-table-column label="发生时间" min-width="180"><template #default="{row}">{{ time(row.occurred_at) }}</template></el-table-column>
      <el-table-column prop="server_name" label="服务器" min-width="150"/>
      <el-table-column label="登录类型" min-width="155"><template #default="{row}"><span class="event-protocol" :title="row.event_type?.startsWith('console.')?consoleHint:loginLabel(row)">{{loginLabel(row)}}</span></template></el-table-column><el-table-column label="用户" min-width="110"><template #default="{row}">{{loginUser(row)}}</template></el-table-column>
      <el-table-column label="国家 / 地区" min-width="150"><template #default="{row}"><span class="event-country"><CountryFlag :code="row.country_code" :name="countryName(row)"/>{{countryName(row)}}</span></template></el-table-column>
      <el-table-column label="来源 IP / 端口" min-width="210"><template #default="{row}">{{sourceAddress(row)}}</template></el-table-column>
      <el-table-column label="认证方式" min-width="110"><template #default="{row}">{{({password:'密码',publickey:'公钥','keyboard-interactive':'交互认证'} as Row)[row.auth_method]||row.auth_method||'—'}}</template></el-table-column>
      <el-table-column label="结果" width="100"><template #default="{row}"><span :class="['status', row.outcome === 'success' ? 'online' : 'failed']">{{ row.outcome === 'success' ? '成功' : '失败' }}</span></template></el-table-column>
    </el-table>
    <el-table v-else-if="active === 'audit-logs'" :data="rows" empty-text="暂无审计记录"><el-table-column label="时间" min-width="185"><template #default="{row}">{{ time(row.created_at) }}</template></el-table-column><el-table-column label="操作人" min-width="120"><template #default="{row}">{{ row.actor || 'Agent / 系统' }}</template></el-table-column><el-table-column label="动作" min-width="150"><template #default="{row}">{{auditNames[row.action]||'其他操作'}}</template></el-table-column><el-table-column label="来源 IP" min-width="150"><template #default="{row}">{{row.source_ip||'—'}}</template></el-table-column><el-table-column label="浏览器 / UA" min-width="210" show-overflow-tooltip><template #default="{row}">{{row.user_agent||'—'}}</template></el-table-column><el-table-column prop="target" label="目标 ID" min-width="300"/></el-table>
    <el-table v-else-if="active === 'users'" :data="rows" empty-text="暂无用户"><el-table-column prop="username" label="用户名"/><el-table-column label="角色"><template #default="{row}">{{roleNames[row.role]||row.role}}</template></el-table-column><el-table-column label="创建时间"><template #default="{row}">{{ time(row.created_at) }}</template></el-table-column></el-table>
    <div v-if="active !== 'dashboard'" class="pagination"><span v-if="active==='servers'" class="page-size-controls">第 {{ page }} 页<el-select v-model="serverPageSize" size="small" popper-class="server-page-size-dropdown" :fit-input-width="false" aria-label="每页显示条数" style="width:115px" @change="changeServerPageSize"><el-option v-for="size in serverPageSizes" :key="size" :label="'每页 '+size+' 条'" :value="size"/></el-select></span><span v-else>第 {{ page }} 页 · 每页 20 条</span><el-button :disabled="page <= 1" @click="page--;load()">上一页</el-button><el-button :disabled="!more" @click="page++;load()">下一页</el-button></div></div>
    <div v-if="(management && active!=='rules') || active==='ip-allowlist'" class="pagination"><span>第 {{ page }} 页 · 每页 20 条</span><el-button :disabled="page<=1" @click="page--;load()">上一页</el-button><el-button :disabled="!more" @click="page++;load()">下一页</el-button></div>
    </section></div>
  </div>
  <el-dialog v-model="serverDialog" :title="server.id ? '编辑服务器' : '添加服务器'" width="min(520px, 94vw)" :close-on-click-modal="false" :close-on-press-escape="true"><el-form label-position="top" @submit.prevent="saveServer"><el-form-item label="服务器名称（可选）"><el-input v-model="server.name" maxlength="128" placeholder="留空自动命名"/></el-form-item><el-form-item label="分组"><el-input v-model="server.group_name" maxlength="64" placeholder="例如：生产环境 / 华东"/></el-form-item><el-form-item label="备注"><el-input v-model="server.notes" type="textarea" maxlength="1024"/></el-form-item><div class="dialog-actions"><el-button @click="serverDialog=false">取消</el-button><el-button type="primary" native-type="submit" :loading="busy">{{server.id ? '保存服务器' : '添加服务器'}}</el-button></div></el-form></el-dialog>
  <el-dialog v-model="userDialog" title="创建用户" width="min(480px, 94vw)" @closed="newUser.password=''" :close-on-click-modal="false" :close-on-press-escape="true"><el-form label-position="top" @submit.prevent="saveUser"><el-form-item label="用户名（3–64 字符）"><el-input v-model="newUser.username"/></el-form-item><el-form-item label="密码（12–72 字节）"><el-input v-model="newUser.password" type="password" show-password/></el-form-item><el-form-item label="角色"><el-select v-model="newUser.role"><el-option label="只读用户" value="viewer"/><el-option label="操作员" value="operator"/><el-option label="管理员" value="admin"/></el-select></el-form-item><div class="dialog-actions"><el-button @click="userDialog=false">取消</el-button><el-button type="primary" native-type="submit" :loading="busy">创建用户</el-button></div></el-form></el-dialog>
  <el-dialog v-model="commandDialog" :title="updating?'更新 Agent':'一键安装 Agent'" width="min(640px,94vw)" class="command-dialog" :close-on-click-modal="false" :close-on-press-escape="true">
   <div class="platform-tabs" role="tablist" aria-label="目标系统"><button v-for="(label,key) in {linux:'Linux',windows:'Windows'}" :key="key" role="tab" :aria-selected="platform===key" :class="{selected:platform===key}" @click="platform=key">{{label}}</button></div>
   <div :class="['command-options',{'update-options':updating}]"><label>主控地址<input :value="commandEndpoint" readonly/></label><label v-if="!updating">上报间隔（秒）<input v-model.number="commandInterval" type="number" min="1" max="600" required/></label></div>
   <el-alert v-if="commandInsecure" title="HTTP 传输未加密，请仅在可信网络使用。" type="warning" :closable="false"/>
   <label class="command-label">{{updating?'更新命令':'安装命令'}}</label><el-input :model-value="commandReady?command:''" :placeholder="commandError?'命令更新失败，请重试':!validCommandInterval?'请输入 1–600 之间的整数':'正在更新安装命令…'" readonly type="textarea" :rows="5" aria-label="部署命令"/>
   <p v-if="commandError" class="captcha-error" role="alert">{{commandError}} <button type="button" class="text-action" @click="refreshEnrollment">重试</button></p>
   <div class="command-meta" role="status"><span v-if="!updating&&commandReady">单次有效 · {{time(expires)}} 到期</span><span v-else-if="!updating">{{validCommandInterval?'安装命令随上报间隔自动更新。':'上报间隔须为 1–600 秒的整数。'}}</span><span v-else>保留身份与队列，失败自动恢复旧程序。</span></div>
   <details class="command-help"><summary>接入说明</summary><p>主控地址须能从目标服务器访问。上报间隔只在安装时写入 Agent 配置，更新保留原值。</p><p v-if="!updating">安装命令含短期凭证，请勿公开转发。重新生成会使旧命令失效。</p><p v-else>Alpine 采集权限修复需在目标服务器执行一次此更新命令。</p></details>
   <el-button class="copy-command" type="primary" :disabled="!commandReady" :loading="!updating&&commandGenerating" @click="copyCommand">{{updating?'复制更新命令':'复制安装命令'}}</el-button>
  </el-dialog>
  <el-dialog v-model="passwordDialog" title="修改密码" width="min(520px, 94vw)" class="password-dialog" :close-on-click-modal="false" @closed="passwords.current_password = ''; passwords.new_password = ''" :close-on-press-escape="true"><el-form label-position="top" @submit.prevent="changePassword"><el-form-item label="当前密码"><el-input v-model="passwords.current_password" type="password" autocomplete="current-password" placeholder="输入当前密码" show-password/></el-form-item><el-form-item label="新密码"><el-input v-model="passwords.new_password" type="password" autocomplete="new-password" placeholder="输入 12–72 字节的新密码" show-password/><span class="field-hint">修改后所有设备上的旧会话都会失效。</span></el-form-item><div class="dialog-actions"><el-button :disabled="busy" @click="passwordDialog=false">取消</el-button><el-button type="primary" native-type="submit" :disabled="!passwords.current_password||!passwords.new_password" :loading="busy">保存并重新登录</el-button></div></el-form></el-dialog>
  <el-dialog :model-value="!!detail" title="事件详情" width="min(700px, 94vw)" @close="detail = null" :close-on-click-modal="false" :close-on-press-escape="true"><pre class="detail">{{ JSON.stringify(detail,null,2) }}</pre></el-dialog>
</template>
