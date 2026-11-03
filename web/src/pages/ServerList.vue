<script setup lang="ts">
import { computed, ref } from 'vue';
import { ElTable, ElTableColumn, ElButton, ElDialog, ElTooltip } from 'element-plus';
import { DocumentCopy, Edit, List, Download } from '@element-plus/icons-vue';
import 'element-plus/es/components/tooltip/style/css';
import AgentMaintenance from '../components/AgentMaintenance.vue';
import CountryFlag from '../components/CountryFlag.vue';
import { copyText } from '../lib/clipboard';
import { ElMessage } from 'element-plus';
type Row = Record<string, any>;
const props=defineProps<{rows: Row[], admin: boolean, writer: boolean, busy: boolean}>();
const emit = defineEmits<{history:[row:Row], edit:[row:Row], enroll:[row:Row], update:[row:Row], revoke:[row:Row], remove:[row:Row], changed:[]}>();
const selectedID = ref('');
const selected = computed<Row|null>({get:()=>props.rows.find(row=>row.id===selectedID.value)||null,set:row=>{selectedID.value=row?.id||''}});
const statusNames: Row = {online:'在线',offline:'离线',pending:'待接入',revoked:'已撤销'};
const stamp = (v:any) => v ? new Date(v).toLocaleString('zh-CN',{hour12:false}) : '—';
function addresses(row:Row):string[] {return Array.isArray(row.public_ip_addresses)?row.public_ip_addresses:[];}
async function copy(value:string) {try{await copyText(value);ElMessage.success('IP 已复制');}catch{ElMessage.warning('请选中 IP 手动复制');}}
function collection(row:Row) {return row.status!=='online'?'等待连接':row.collector_state==='healthy'?'采集正常':row.collector_state==='error'?'采集异常':'等待采集';}
</script>
<template>
 <el-table class="server-table" :data="rows" stripe row-key="id" empty-text="暂无服务器">
  <el-table-column label="名称" min-width="180"><template #default="{row}"><button class="server-name server-name-with-flag" @click="selected=row"><CountryFlag :code="row.country_code" :name="row.country_name"/><span>{{row.name}}</span></button></template></el-table-column>
  <el-table-column label="公网 IP 地址" min-width="200"><template #default="{row}"><div v-for="address in addresses(row).slice(0,2)" :key="address" class="server-ip"><span :title="address">{{address}}</span><el-button text :icon="DocumentCopy" :aria-label="'复制 IP '+address" @click="copy(address)"/></div><button v-if="addresses(row).length>2" class="text-action" @click="selected=row">另 {{addresses(row).length-2}} 个公网地址</button><span v-if="!addresses(row).length" class="subtle">等待识别公网 IP</span></template></el-table-column>
  <el-table-column prop="agent_version" label="客户端版本" width="110"/>
  <el-table-column label="分组" min-width="95"><template #default="{row}">{{row.group_name||'—'}}</template></el-table-column>
  <el-table-column label="备注" min-width="130" show-overflow-tooltip><template #default="{row}">{{row.notes||'—'}}</template></el-table-column>
  <el-table-column label="状态" width="165"><template #default="{row}"><div class="server-state"><span :class="['status',row.status]">{{statusNames[row.status]}}</span><button :class="['collection-button', {warning:row.collector_state==='error'}]" @click="selected=row">{{collection(row)}}</button></div></template></el-table-column>
  <el-table-column label="最近上报" min-width="160"><template #default="{row}"><span>{{stamp(row.last_seen_at)}}</span></template></el-table-column>
  <el-table-column label="操作" width="172" fixed="right"><template #default="{row}"><div class="server-actions">
   <el-tooltip content="查看事件"><el-button text :icon="List" aria-label="查看事件" @click="emit('history',row)"/></el-tooltip>
   <el-tooltip v-if="writer" content="编辑"><el-button text :icon="Edit" aria-label="编辑服务器" @click="emit('edit',row)"/></el-tooltip>
   <el-tooltip v-if="admin" content="安装命令"><el-button text :icon="Download" aria-label="安装命令" :disabled="busy" @click="emit('enroll',row)"/></el-tooltip>
   <AgentMaintenance v-if="admin" :server="row" :disabled="busy" @changed="emit('changed')" @manual="emit('update',row)" @revoke="emit('revoke',row)" @remove="emit('remove',row)"/>
  </div></template></el-table-column>
 </el-table>
 <el-dialog :model-value="!!selected" :title="selected?.name||'服务器详情'" width="min(600px,94vw)" append-to-body @close="selected=null" :close-on-click-modal="false" :close-on-press-escape="true">
  <template v-if="selected"><dl class="server-facts"><dt>系统</dt><dd>{{selected.distribution||'—'}}</dd><template v-if="selected.country_code"><dt>国家 / 地区</dt><dd class="server-country"><CountryFlag :code="selected.country_code" :name="selected.country_name"/>{{selected.country_name||selected.country_code}}</dd></template><dt>公网 IP 地址</dt><dd><div v-for="address in addresses(selected)" :key="address">{{address}}</div>{{addresses(selected).length?'':'等待识别公网 IP'}}</dd><dt>采集状态</dt><dd>{{collection(selected)}}</dd><dt>队列</dt><dd>待传 {{selected.queue_depth||0}} · 隔离 {{selected.rejected_events||0}}</dd><dt>上报间隔</dt><dd>{{selected.report_interval_seconds}} 秒 <span v-if="!selected.reported_interval_seconds">（需更新 Agent）</span><span v-else-if="selected.reported_interval_seconds!==selected.report_interval_seconds">（安装设置；当前 Agent 上报 {{selected.reported_interval_seconds}} 秒）</span></dd><dt>最近上报</dt><dd>{{stamp(selected.last_seen_at)}}</dd></dl>
   <div v-if="selected.collector_message" class="diagnostic"><h3>采集异常详情</h3><p v-if="/durable Agent state/i.test(selected.collector_message)">Agent 本地状态数据库初始化失败。请根据下方错误检查数据库占用、状态目录权限或磁盘空间，然后重启 Agent。</p><p v-else-if="/unreadable|permission|no SSH log|journal unavailable/i.test(selected.collector_message)">日志不存在或当前账号没有读取权限。请在目标服务器执行一次最新的更新命令；Alpine 会配置专用认证日志。仍有异常时，请检查 sshd 和日志服务。</p><pre>{{selected.collector_message}}</pre><el-button v-if="admin" @click="emit('update',selected);selected=null">获取更新命令</el-button></div>
  </template>
 </el-dialog>
</template>
<style scoped>.server-name-with-flag,.server-country{display:inline-flex;align-items:center;gap:10px}.server-name-with-flag{text-align:left;max-width:100%}.server-name-with-flag span{overflow-wrap:anywhere}</style>
