<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue';
import { ElDialog, ElMessage, ElMessageBox } from 'element-plus';
type Row = Record<string,any>;
const props=defineProps<{mode:string, rows:Row[], admin:boolean, writer:boolean, api:(path:string,method?:string,body?:any)=>Promise<any>}>();
const emit=defineEmits<{refresh:[]}>();
const dialog=ref(false),saving=ref(false),selected=ref<Row|null>(null),available=ref<Row[]>([]);
const form=reactive<Row>({}),config=reactive<Row>({}),configured=ref<Row>({}),trusted=ref(''),recipients=ref('');
const kinds:Row={webhook:'Webhook',wecom:'企业微信',dingtalk:'钉钉',feishu:'飞书',telegram:'Telegram',smtp:'邮件 SMTP'};
const rules:Row={login:'登录成功',root:'特权账户登录',failure_threshold:'连续认证失败',agent_offline:'Agent 离线',collector_error:'采集异常'};
const statuses:Row={pending:'等待重试 / 投递',sending:'正在投递',sent:'已送达',dead:'需要处理'};
const deliveryErrors:Row={'channel disabled':'通知渠道已停用，请启用后重试','delivery attempts exhausted':'已达到最大重试次数，请检查渠道配置后重试','channel decryption failed; restore the encryption key':'无法解密通知渠道配置，请恢复原加密密钥','delivery failed; verify channel credentials, TLS, destination policy and provider status':'投递失败，请检查渠道凭证、TLS、目标访问规则及服务商状态'};
const stamp=(s:any)=>s?new Date(s).toLocaleString('zh-CN',{hour12:false}):'—';
const isChannel=computed(()=>props.mode==='channels');
const checked=ref<string[]>([]);
const pending=computed(()=>props.rows.filter(row=>!row.acknowledged_at&&!row.resolved_at));
const allChecked=computed(()=>pending.value.length>0&&pending.value.every(row=>checked.value.includes(row.id)));
watch(()=>[props.mode,props.rows],()=>{checked.value=checked.value.filter(id=>pending.value.some(row=>row.id===id));});
function toggleAll(event:Event){checked.value=(event.target as HTMLInputElement).checked?pending.value.map(row=>row.id):[];}
async function acknowledgeBatch(all=false){
 if(saving.value||(!all&&!checked.value.length))return;
 if(all){try{await ElMessageBox.confirm('将确认所有分页中的待处理告警，已确认和已恢复的告警会自动跳过。','确认全部待处理告警',{confirmButtonText:'全部确认',cancelButtonText:'取消',type:'warning',closeOnClickModal:false,closeOnPressEscape:true});}catch{return;}}
 await perform(async()=>{const result=await props.api('/alerts/acknowledge','POST',all?{all:true}:{ids:[...checked.value]});checked.value=[];emit('refresh');ElMessage.success(result.acknowledged?`已确认 ${result.acknowledged} 条告警`:'没有需要确认的告警');});
}
const configFields=computed(()=>form.kind==='smtp' ? [{key:'host',label:'SMTP 主机',type:'text'},{key:'port',label:'端口',type:'number'},{key:'username',label:'登录用户名',type:'text'},{key:'password',label:'登录密码',type:'password'},{key:'from',label:'发件地址',type:'email'}] : form.kind==='telegram' ? [{key:'bot_token',label:'Bot Token',type:'password'},{key:'chat_id',label:'Chat ID',type:'text'}] : [{key:'url',label:'HTTPS Webhook 地址',type:'password'},...(['webhook','dingtalk','feishu'].includes(form.kind)?[{key:'secret',label:'签名密钥',type:'password'}]:[])]);
async function perform(fn:()=>Promise<void>){saving.value=true;try{await fn()}catch(e){ElMessage.error((e as Error).message)}finally{saving.value=false}}
function reset(){Object.keys(form).forEach(k=>delete form[k]);Object.keys(config).forEach(k=>delete config[k]);configured.value={};trusted.value='';recipients.value='';}
async function edit(row?:Row){await perform(async()=>{
 reset();
 if(isChannel.value){
  const value=row?await props.api('/channels/'+row.id):{name:'',kind:'webhook',enabled:true,config:{port:465,tls_mode:'tls'}};
  Object.assign(form,{id:value.id,name:value.name,kind:value.kind,enabled:!!value.enabled,replace_secrets:false});
  Object.assign(config,value.config);configured.value=value.configured||{};recipients.value=(config.to||[]).join('\n');
 } else {
  Object.assign(form,row?{...row,enabled:!!row.enabled,channel_ids:[...(row.channel_ids||[])]}:{name:'',kind:'login',enabled:true,threshold_count:1,window_seconds:300,cooldown_seconds:60,server_group:'',channel_ids:[]});
  trusted.value=(form.trusted_cidrs||[]).join('\n');available.value=(await props.api('/channels?page_size=100')).items;
 }
 dialog.value=true;
})}
async function save(){await perform(async()=>{
 const body=isChannel.value?{...form,config:{...config,to:recipients.value.split(/[\n,]+/).map(v=>v.trim()).filter(Boolean)}}:{...form,trusted_cidrs:trusted.value.split(/[\n,]+/).map(v=>v.trim()).filter(Boolean)};
 await props.api('/'+props.mode+(form.id?'/'+form.id:''),form.id?'PATCH':'POST',body);
 dialog.value=false;reset();emit('refresh');ElMessage.success('配置已保存');
})}
async function test(row:Row){await perform(async()=>{await props.api(`/channels/${row.id}/test`,'POST',{});ElMessage.success('测试通知已入队，请在投递记录中查看结果');emit('refresh')})}
async function acknowledge(row:Row){await perform(async()=>{await props.api(`/alerts/${row.id}/acknowledge`,'POST',{});checked.value=checked.value.filter(id=>id!==row.id);emit('refresh')})}
async function retry(row:Row){await perform(async()=>{await props.api(`/notifications/${row.id}/retry`,'POST',{});emit('refresh');ElMessage.success('已重新加入投递队列')})}
</script>
<template>
 <section class="panel management-panel">
  <div class="panel-heading"><div><h3>{{ mode==='channels'?'通知渠道':mode==='rules'?'检测规则':mode==='alerts'?'安全告警':'通知投递记录' }}</h3></div><button v-if="admin && ['channels','rules'].includes(mode)" class="primary-action" :disabled="saving" @click="edit()">＋ {{ mode==='channels'?'添加渠道':'新建规则' }}</button></div>
  <div v-if="mode==='alerts'&&writer" class="alert-batch-toolbar"><span aria-live="polite">已选择 <b>{{checked.length}}</b> 条告警</span><button class="text-action" :disabled="saving||!checked.length" @click="checked=[]">取消选择</button><div class="alert-batch-actions"><button class="secondary-action" :disabled="saving||!rows.length" @click="acknowledgeBatch(true)">全部确认</button><button class="primary-action" :disabled="saving||!checked.length" @click="acknowledgeBatch()">{{saving?'正在确认…':'批量确认'}}<template v-if="checked.length">（{{checked.length}}）</template></button></div></div>
  <div v-if="!rows.length" class="empty-state"><div class="empty-symbol">◇</div><b>{{ mode==='channels'?'尚未配置通知渠道':mode==='notifications'?'暂无投递记录':mode==='rules'?'暂无检测规则':'暂无告警记录' }}</b></div>
  <div v-else class="table-scroll"><table class="data-table">
   <thead><tr v-if="mode==='channels'"><th>渠道名称</th><th>类型</th><th>状态</th><th>更新于</th><th>操作</th></tr><tr v-else-if="mode==='rules'"><th>规则名称</th><th>检测条件</th><th>作用范围</th><th>冷却时间</th><th>操作</th></tr><tr v-else-if="mode==='alerts'"><th v-if="writer" class="alert-check-cell"><input type="checkbox" :checked="allChecked" :indeterminate="checked.length>0&&!allChecked" :disabled="saving||!pending.length" aria-label="选择本页全部待处理告警" @change="toggleAll"/></th><th>告警</th><th>服务器</th><th>级别</th><th>发生时间</th><th>状态 / 操作</th></tr><tr v-else><th>告警 / 渠道</th><th>投递状态</th><th>已尝试</th><th>下次尝试 / 送达时间</th><th>操作</th></tr></thead>
   <tbody><tr v-for="row in rows" :key="row.id" :class="{'alert-row-selected':mode==='alerts'&&checked.includes(row.id)}">
    <td v-if="mode==='alerts'&&writer" class="alert-check-cell"><input v-model="checked" type="checkbox" :value="row.id" :disabled="saving||!!row.acknowledged_at||!!row.resolved_at" :aria-label="`选择告警：${row.title}`"/></td>
    <template v-if="mode==='channels'"><td><b>{{ row.name }}</b></td><td>{{ kinds[row.kind] }}</td><td><span :class="['status',row.enabled?'online':'']">{{ row.enabled?'已启用':'已停用' }}</span></td><td>{{ stamp(row.updated_at) }}</td><td><button class="text-action" :disabled="saving" @click="edit(row)">编辑</button><button class="text-action" :disabled="saving || !row.enabled" @click="test(row)">发送测试</button></td></template>
    <template v-else-if="mode==='rules'"><td><b>{{ row.name }}</b><small>{{ row.enabled?'已启用':'已停用' }}</small></td><td>{{ rules[row.kind] }}<small v-if="row.kind==='failure_threshold'">{{ row.window_seconds }} 秒内 {{ row.threshold_count }} 次</small><small v-if="['agent_offline','collector_error'].includes(row.kind)">持续 {{ row.threshold_count }} 秒</small></td><td>{{ row.server_group || '全部服务器' }}</td><td>{{ row.cooldown_seconds }} 秒</td><td><button class="text-action" :disabled="saving" @click="edit(row)">编辑规则</button></td></template>
    <template v-else-if="mode==='alerts'"><td><button class="text-action strong" @click="selected=row">{{ row.title }}</button></td><td>{{ row.server_name || '系统测试' }}</td><td><span :class="['status',row.severity==='high'?'failed':'']">{{ row.severity==='high'?'高风险':'提示' }}</span></td><td>{{ stamp(row.created_at) }}</td><td><span v-if="row.resolved_at" class="status online">已恢复</span><span v-else-if="row.acknowledged_at" class="status">已确认</span><button v-else-if="writer" class="text-action" :disabled="saving" @click="acknowledge(row)">确认处理</button><span v-else class="status pending">待处理</span></td></template>
    <template v-else><td><b>{{ row.alert_title }}</b><small>{{ row.channel_name }}</small></td><td><span :class="['status',row.status==='sent'?'online':row.status==='dead'?'failed':'pending']">{{ statuses[row.status] }}</span><small v-if="row.last_error">{{ deliveryErrors[row.last_error]||row.last_error }}</small></td><td>{{ row.attempts }} / 8</td><td>{{ stamp(row.sent_at || (row.status==='dead'?null:row.next_attempt_at)) }}</td><td><button v-if="row.status==='dead'" class="text-action" :disabled="saving" @click="retry(row)">重试投递</button><span v-else class="subtle">—</span></td></template>
   </tr></tbody>
  </table></div>
 </section>
 <el-dialog v-model="dialog" :title="(form.id?'编辑':'添加')+(isChannel?'通知渠道':'检测规则')" width="min(640px, 94vw)" @closed="reset" :close-on-click-modal="false" :close-on-press-escape="true">
  <form class="editor" @submit.prevent="save">
   <label>名称<input v-model="form.name" required maxlength="128" placeholder="清楚描述用途，例如：生产环境登录通知"/></label>
   <div class="form-grid"><label>类型<select v-model="form.kind" :disabled="isChannel && !!form.id"><option v-for="(label,key) in (isChannel?kinds:rules)" :key="key" :value="key">{{ label }}</option></select></label><label class="check-label"><input v-model="form.enabled" type="checkbox"/>启用此{{ isChannel?'渠道':'规则' }}</label></div>
   <template v-if="isChannel">
    <p v-if="form.id" class="form-note">已保存的密钥不会回显。密钥字段留空将保留原值。</p>
    <label v-for="field in configFields" :key="field.key">{{ field.label }}<input v-if="field.type==='number'" v-model.number="config[field.key]" type="number" min="1" max="65535" required/><input v-else v-model="config[field.key]" :type="field.type" autocomplete="off" :placeholder="configured[field.key]?'已配置，留空保持不变':''" maxlength="2048"/></label>
    <template v-if="form.kind==='smtp'"><label>传输安全<select v-model="config.tls_mode"><option value="tls">TLS（通常 465）</option><option value="starttls">STARTTLS（通常 587）</option></select></label><label>收件地址<textarea v-model="recipients" rows="3" placeholder="每行一个邮箱，最多 20 个" required/></label></template>
    <label v-if="form.id" class="check-label"><input v-model="form.replace_secrets" type="checkbox"/>替换全部密钥（勾选后，空值将清除原密钥）</label>
   </template>
   <template v-else>
    <label>服务器分组<input v-model="form.server_group" maxlength="64" placeholder="留空表示全部服务器；填写时须与服务器分组一致"/></label>
    <div class="form-grid"><label>阈值{{ ['agent_offline','collector_error'].includes(form.kind)?'（秒）':'（次数）' }}<input v-model.number="form.threshold_count" type="number" min="1" max="10000" required/></label><label>统计窗口（秒）<input v-model.number="form.window_seconds" type="number" min="30" max="86400" required/></label><label>冷却时间（秒）<input v-model.number="form.cooldown_seconds" type="number" min="30" max="86400" required/></label></div>
    <label>规则 IP 白名单<textarea v-model="trusted" rows="3" placeholder="每行一个 IP 或网段，例如 192.0.2.1 或 2001:db8::/32"/></label>
    <label>投递渠道<select v-model="form.channel_ids" multiple size="4"><option v-for="channel in available" :key="channel.id" :value="channel.id">{{ channel.name }}{{ channel.enabled?'':'（已停用）' }}</option></select><small>未选择时使用所有已启用渠道。按 Ctrl / Command 多选。</small></label>
   </template>
   <div class="dialog-actions"><button type="button" class="secondary-action" @click="dialog=false">取消</button><button class="primary-action" type="submit" :disabled="saving">{{ saving?'保存中…':'保存配置' }}</button></div>
  </form>
 </el-dialog>
 <el-dialog :model-value="!!selected" title="告警详情" width="min(640px,94vw)" @close="selected=null" :close-on-click-modal="false" :close-on-press-escape="true"><template v-if="selected"><h3>{{ selected.title }}</h3><p>{{ stamp(selected.created_at) }} · {{ selected.server_name || '系统' }}</p><pre class="detail">{{ selected.message }}</pre></template></el-dialog>
</template>
