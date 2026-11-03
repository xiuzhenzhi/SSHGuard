<script setup lang="ts">
import { reactive, ref } from 'vue';
import { ElDialog, ElMessage, ElMessageBox } from 'element-plus';
type Row=Record<string,any>;
const props=defineProps<{rows:Row[],api:(path:string,method?:string,body?:any)=>Promise<any>}>();
const emit=defineEmits<{refresh:[]}>();
const visible=ref(false),busy=ref(false),form=reactive({cidr:'',notes:''});
async function save(){busy.value=true;try{await props.api('/ip-allowlist','POST',form);visible.value=false;form.cidr='';form.notes='';emit('refresh');ElMessage.success('已加入白名单');}catch(e){ElMessage.error((e as Error).message)}finally{busy.value=false}}
async function remove(row:Row){try{await ElMessageBox.confirm(`移除 ${row.cidr} 后，该来源将按规则触发告警。`,'移除白名单',{confirmButtonText:'移除',cancelButtonText:'取消',type:'warning',closeOnClickModal:false,closeOnPressEscape:true})}catch{return}busy.value=true;try{await props.api('/ip-allowlist/'+row.id,'DELETE');emit('refresh')}catch(e){ElMessage.error((e as Error).message)}finally{busy.value=false}}
</script>
<template>
 <section class="panel"><div class="panel-heading"><p class="allowlist-description">白名单来源不触发 登录事件告警，登录记录仍会保留。</p><button class="primary-action" @click="visible=true">添加 IP</button></div>
 <div v-if="!rows.length" class="empty-state">暂无白名单</div>
 <div v-else class="table-scroll"><table class="data-table"><thead><tr><th>IP / 网段</th><th>备注</th><th>操作</th></tr></thead><tbody><tr v-for="row in rows" :key="row.id"><td>{{row.cidr}}</td><td>{{row.notes||'—'}}</td><td><button class="text-action danger-action" :disabled="busy" @click="remove(row)">移除</button></td></tr></tbody></table></div>
 </section>
 <el-dialog v-model="visible" title="添加 IP 白名单" width="min(460px,94vw)" :close-on-click-modal="false" :close-on-press-escape="true"><form class="editor" @submit.prevent="save"><label>IP 地址或网段<input v-model="form.cidr" placeholder="192.0.2.1 或 2001:db8::/32" required maxlength="64"/></label><label>备注<input v-model="form.notes" maxlength="256"/></label><div class="dialog-actions"><button type="button" class="secondary-action" @click="visible=false">取消</button><button class="primary-action" :disabled="busy">保存</button></div></form></el-dialog>
</template>
