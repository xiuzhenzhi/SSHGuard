<script setup lang="ts">
import { computed, ref } from 'vue';
import { requestJSON } from '../lib/api';
import { copyText } from '../lib/clipboard';
import { ElButton, ElDialog, ElAlert, ElInput, ElSelect, ElOption, ElMessage, ElMessageBox, ElDropdown, ElDropdownMenu, ElDropdownItem } from 'element-plus';
import { MoreFilled } from '@element-plus/icons-vue';
import 'element-plus/es/components/dropdown/style/css';
import 'element-plus/es/components/dropdown-menu/style/css';
import 'element-plus/es/components/dropdown-item/style/css';
const props = defineProps<{server: Record<string, any>, disabled?: boolean}>();
const emit = defineEmits<{changed: [], manual: [], revoke: [], remove: []}>();
const busy = ref(false), visible = ref(false), platform = ref('linux');
const commands = ref<Record<string,string>>({});
const activeTask = computed(() => ['pending','running'].includes(props.server.update_status) && new Date(props.server.update_expires_at).getTime()>Date.now());
const state = computed(() => {
  if (props.server.agent_version === props.server.available_agent_version && props.server.available_agent_version) return 'Agent 已是当前版本';
  if (['pending','running'].includes(props.server.update_status) && !activeTask.value) return '更新任务已超时，请检查维护服务';
  return {idle:'更新服务就绪',pending:'等待更新',running:'正在更新',success:'更新成功',failed:'更新失败'}[props.server.update_status as string] || '需手动更新以启用远程更新';
});
const current = computed(() => props.server.agent_version === props.server.available_agent_version);
const ready = computed(() => props.server.updater_seen_at && Date.now()-new Date(props.server.updater_seen_at).getTime()<180000);
async function request(path:string,method='GET') {
  return requestJSON(path, method, method === 'POST' ? {} : undefined);
}
async function update() {
  try { await ElMessageBox.confirm(`将 ${props.server.name} 的 Agent 更新到平台提供的版本。更新期间采集会短暂重启，失败时尝试恢复原程序。`, '更新 Agent', {confirmButtonText:'开始更新',cancelButtonText:'取消',type:'warning',closeOnClickModal:false,closeOnPressEscape:true}); } catch {return;}
  busy.value=true;
  try {await request(`/servers/${props.server.id}/update`,'POST');ElMessage.success('更新任务已创建，目标机器将在下一次检查时处理');emit('changed');} catch(e){ElMessage.error((e as Error).message);}finally{busy.value=false;}
}
async function uninstall() {
  busy.value=true;
  try {commands.value=(await request('/agent/uninstall-commands')).commands;platform.value=/windows/i.test(props.server.distribution)?'windows':'linux';visible.value=true;}catch(e){ElMessage.error((e as Error).message);}finally{busy.value=false;}
}
async function copy() {try{await copyText(commands.value[platform.value]||'');ElMessage.success('卸载命令已复制');}catch{ElMessage.warning('请手动复制命令');}}
function handleCommand(key:string) {
  if(busy.value||props.disabled)return;
  if(key==='update')void update();
  else if(key==='manual')emit('manual');
  else if(key==='revoke')emit('revoke');
  else if(key==='uninstall')void uninstall();
  else if(key==='delete')emit('remove');
}
</script>
<template>
  <el-dropdown trigger="click" :disabled="busy||disabled" @command="handleCommand">
   <el-button text :icon="MoreFilled" aria-label="更多维护操作" :disabled="busy||disabled"/>
   <template #dropdown><el-dropdown-menu>
    <el-dropdown-item disabled>{{state}}</el-dropdown-item>
    <el-dropdown-item command="update" :disabled="server.status==='revoked'||!ready||activeTask||current">平台更新</el-dropdown-item>
    <el-dropdown-item command="manual">更新命令</el-dropdown-item>
    <el-dropdown-item command="uninstall" divided>卸载命令</el-dropdown-item>
    <el-dropdown-item command="revoke" :disabled="server.status==='revoked'">撤销凭证</el-dropdown-item>
    <el-dropdown-item command="delete" divided style="color:var(--el-color-danger)">删除服务器</el-dropdown-item>
   </el-dropdown-menu></template>
  </el-dropdown>
  <el-dialog v-model="visible" title="卸载 Agent" width="min(780px, 94vw)" append-to-body @closed="commands = {}" :close-on-click-modal="false" :close-on-press-escape="true">
    <el-alert title="只生成命令，不会远程卸载。请在目标机器以管理员身份执行。" type="warning" :closable="false"/>
    <p>卸载将永久清除本机 Agent 配置、凭据、采集队列、采集位置及专用日志，并移除程序和更新服务。中央历史记录可通过「删除服务器」清除；系统共享日志不会整库清空。</p>
    <el-select v-model="platform" aria-label="目标操作系统"><el-option label="Linux（Ubuntu / Debian / Alpine）" value="linux"/><el-option label="Windows（管理员 PowerShell）" value="windows"/></el-select>
    <el-input :model-value="commands[platform] || ''" type="textarea" :rows="7" readonly aria-label="卸载命令" style="margin:16px 0"/>
    <el-button type="primary" @click="copy">复制卸载命令</el-button>
  </el-dialog>
</template>
