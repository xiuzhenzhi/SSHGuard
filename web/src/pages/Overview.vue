<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue';
import AnimatedNumber from '../components/AnimatedNumber.vue';
import CountryRanking from '../components/CountryRanking.vue';
import RecentEvents from '../components/RecentEvents.vue';
const LoginTrend=defineAsyncComponent(()=>import('../components/LoginTrend.vue'));
const props = defineProps<{summary: Record<string,any>, overview: Record<string,any>, admin: boolean, range:string, trendLoading:boolean, api:(path:string,method?:string,body?:any,signal?:AbortSignal)=>Promise<any>}>();
const emit = defineEmits<{navigate: [page: string],range:[value:string],showEvent:[event:Record<string,any>],allEvents:[]}>();
const stamp = (value: string) => new Date(value).toLocaleTimeString('zh-CN',{hour:'2-digit',minute:'2-digit'});
const healthy = computed(() => props.overview.healthy_collectors || 0);
const percent = computed(() => props.summary.servers ? Math.min(100,healthy.value/props.summary.servers*100) : 0);
</script>
<template>
  <div class="overview">
    <div class="metrics">
      <article><div>在线服务器 <span class="metric-icon">◉</span></div><strong><AnimatedNumber :value="summary.online"/><em> / {{ summary.servers || 0 }}</em></strong></article>
      <article><div>24 小时成功登录 <span class="metric-icon">↗</span></div><strong><AnimatedNumber :value="summary.success_24h"/></strong></article>
      <article><div>24 小时认证失败 <span class="metric-icon warm">↘</span></div><strong><AnimatedNumber :value="summary.failure_24h"/></strong></article>
      <article :class="{attention:overview.unacknowledged_alerts}"><div>待处理告警 <span class="metric-icon warm">!</span></div><strong><AnimatedNumber :value="overview.unacknowledged_alerts"/></strong></article>
    </div>
    <div class="overview-grid">
      <LoginTrend :buckets="overview.range===range ? overview.trend||[] : []" :range="range" :loading="trendLoading && overview.range!==range" @range="value=>emit('range',value)"/>
      <section class="panel health-panel"><div class="panel-heading"><h3>采集与来源</h3><span class="subtle">运行状态</span></div><div class="health-content"><div class="health-ring" :style="{'--progress':percent+'%'}"><div><strong><AnimatedNumber :value="healthy"/></strong><small>采集正常</small></div></div><div class="health-caption">{{ summary.servers || 0 }} 台服务器中，{{ healthy }} 台正在正常采集</div><CountryRanking :api="api" :refresh-key="overview"/></div></section>
    </div>
    <div class="recent-activity-grid">
      <section class="panel recent-alerts"><div class="panel-heading"><h3>最近告警</h3><button class="text-action" @click="emit('navigate','alerts')">查看全部 →</button></div><div v-if="!overview.recent_alerts?.length" class="empty-state"><b>暂无告警记录</b></div><button v-for="item in overview.recent_alerts" :key="item.id" class="alert-preview" @click="emit('navigate','alerts')"><span :class="['alert-indicator',item.severity]"></span><div><b>{{ item.title }}</b><small>{{ item.server_name || '系统测试' }}</small></div><span :class="['status',item.resolved_at ? 'online' : item.acknowledged_at ? '' : 'pending']">{{ item.resolved_at ? '已恢复' : item.acknowledged_at ? '已确认' : '待处理' }}</span><time>{{ stamp(item.created_at) }}</time></button></section>
      <RecentEvents :api="api" :refresh-key="overview" @select="event=>emit('showEvent',event)" @all="emit('allEvents')"/>
    </div>
  </div>
</template>
<style scoped>
.recent-activity-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:22px;margin-bottom:20px}.recent-activity-grid .recent-alerts{min-width:0;margin-bottom:0;overflow:hidden}.recent-activity-grid .alert-preview{min-height:78px}.recent-activity-grid .alert-preview>div{min-width:0}.recent-activity-grid .alert-preview b,.recent-activity-grid .alert-preview small{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;display:block}.recent-activity-grid .alert-preview .status{flex-shrink:0}.recent-activity-grid .alert-preview time{flex-shrink:0}@media(max-width:1100px){.recent-activity-grid{grid-template-columns:1fr}}@media(prefers-reduced-motion:no-preference){.recent-activity-grid>.panel{animation:rise-in .3s ease both}.recent-activity-grid>.panel:last-child{animation-delay:.07s}}
</style>
