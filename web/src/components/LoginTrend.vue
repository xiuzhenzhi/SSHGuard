<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue';
import * as echarts from 'echarts/core';
import { LineChart } from 'echarts/charts';
import { GridComponent, TooltipComponent, AriaComponent } from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import AnimatedNumber from './AnimatedNumber.vue';
import { isDark } from '../lib/theme';

echarts.use([LineChart,GridComponent,TooltipComponent,AriaComponent,CanvasRenderer]);
type Bucket={at:string,success:number,failure:number};
const props=defineProps<{buckets:Bucket[],range:string}>();
const emit=defineEmits<{range:[value:string]}>();
const host=ref<HTMLElement|null>(null),active=ref<number|null>(null);
const totals=computed(()=>props.buckets.reduce((v,b)=>({success:v.success+b.success,failure:v.failure+b.failure}),{success:0,failure:0}));
const motion=window.matchMedia('(prefers-reduced-motion: reduce)');
let chart:echarts.ECharts|undefined,observer:ResizeObserver|undefined;
const label=(at:string)=>new Date(at).toLocaleString('zh-CN',props.range==='24h'?{hour:'2-digit',minute:'2-digit'}:{month:'2-digit',day:'2-digit',timeZone:'UTC'});
function hideTip(){active.value=null;chart?.dispatchAction({type:'hideTip'});chart?.dispatchAction({type:'updateAxisPointer',currTrigger:'leave'});}
function outside(event:PointerEvent){if(host.value&&!host.value.contains(event.target as Node))hideTip();}
function visible(){if(document.hidden)hideTip();}
function move(delta:number){if(!props.buckets.length)return;active.value=Math.max(0,Math.min(props.buckets.length-1,(active.value??-1)+delta));chart?.dispatchAction({type:'showTip',seriesIndex:0,dataIndex:active.value});}
function render(){
 if(!chart)return;
 const dark=isDark.value,foreground=dark?'#c7ccdc':'#657089',grid=dark?'#343846':'#e7e9ee';
 const colors=dark?['#19c99a','#628fff']:['#10b98a','#3b78ff'];
 const animate=!motion.matches&&!document.hidden&&!document.documentElement.classList.contains('theme-transition');
 chart.setOption({
  animation:animate,animationDuration:700,animationDurationUpdate:animate?500:0,animationEasing:'cubicOut',animationEasingUpdate:'cubicInOut',
  aria:{enabled:true,label:{description:`服务器登录趋势，成功 ${totals.value.success} 次，失败 ${totals.value.failure} 次。`}},
  grid:{left:46,right:20,top:20,bottom:30,containLabel:false},
  tooltip:{trigger:'axis',triggerOn:'mousemove|click',confine:true,alwaysShowContent:false,enterable:false,hideDelay:60,transitionDuration:.12,backgroundColor:dark?'#292d3b':'#ffffff',borderColor:dark?'#3b4050':'#e9e7ef',textStyle:{color:foreground,fontSize:12},padding:[13,16],extraCssText:'border-radius:8px;box-shadow:0 5px 22px #00000020;pointer-events:none;',
   axisPointer:{type:'line',animation:!motion.matches,lineStyle:{color:dark?'#928ab8':'#b1aac9',type:'dashed'}},
   formatter:(params:any)=>{const index=Array.isArray(params)?params[0]?.dataIndex:params?.dataIndex;const bucket=props.buckets[index];if(!bucket)return '';active.value=index;const stamp=props.range==='24h'?new Date(bucket.at).toLocaleString('zh-CN',{month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit'}):label(bucket.at);return `<div style="min-width:145px"><b>${stamp}</b><div style="display:flex;justify-content:space-between;margin:10px 0;padding-bottom:9px;border-bottom:1px solid ${grid}"><span>合计</span><b>${bucket.success+bucket.failure}</b></div>${['成功登录','认证失败'].map((name,i)=>`<div style="display:flex;justify-content:space-between;gap:22px;margin-top:8px"><span><i style="display:inline-block;width:7px;height:7px;background:${colors[i]};border-radius:2px;margin-right:7px"></i>${name}</span><b>${i===0?bucket.success:bucket.failure}</b></div>`).join('')}</div>`;}
  },
  xAxis:{type:'category',boundaryGap:false,data:props.buckets.map(b=>b.at),axisLine:{show:false},axisTick:{show:false},axisLabel:{color:foreground,fontSize:11,hideOverlap:true,formatter:(value:string)=>label(value),margin:15}},
  yAxis:{type:'value',min:0,minInterval:1,splitNumber:4,axisLine:{show:false},axisTick:{show:false},axisLabel:{color:foreground,fontSize:11},splitLine:{lineStyle:{color:grid,type:'dashed'}}},
  series:([{kind:'success',name:'成功登录'},{kind:'failure',name:'认证失败'}] as const).map((item,i)=>({
   id:item.kind,name:item.name,type:'line',smooth:.3,smoothMonotone:'x',showSymbol:props.buckets.length<31,symbol:'circle',symbolSize:5,clip:true,
   data:props.buckets.map(b=>({name:b.at,value:b[item.kind]})),itemStyle:{color:colors[i]},lineStyle:{color:colors[i],width:2.3},
   areaStyle:{color:new echarts.graphic.LinearGradient(0,0,0,1,[{offset:0,color:i===0?(dark?'#19c99a36':'#10b98a26'):(dark?'#628fff46':'#3b78ff36')},{offset:1,color:i===0?'#10b98a04':'#3b78ff08'}])},emphasis:{focus:'none',scale:1.5}
  }))
 },{lazyUpdate:false});
}
watch(()=>[props.range,props.buckets.map(b=>`${b.at}:${b.success}:${b.failure}`).join('|'),isDark.value],()=>{hideTip();render();});
onMounted(()=>{
 if(!host.value)return;chart=echarts.init(host.value,undefined,{renderer:'canvas'});render();
 observer=new ResizeObserver(()=>chart?.resize({animation:{duration:0}}));observer.observe(host.value);
 window.addEventListener('pointermove',outside,{passive:true});window.addEventListener('pointerover',outside,{passive:true});window.addEventListener('pointerdown',outside,{passive:true});window.addEventListener('blur',hideTip);document.addEventListener('visibilitychange',visible);motion.addEventListener('change',render);
});
onUnmounted(()=>{observer?.disconnect();window.removeEventListener('pointermove',outside);window.removeEventListener('pointerover',outside);window.removeEventListener('pointerdown',outside);window.removeEventListener('blur',hideTip);document.removeEventListener('visibilitychange',visible);motion.removeEventListener('change',render);chart?.dispose();chart=undefined;});
</script>
<template>
 <section class="panel trend-panel"><div class="panel-heading trend-heading" @pointerenter="hideTip"><h3>登录趋势</h3><div class="trend-inline-totals"><span>成功登录 <b><AnimatedNumber :value="totals.success"/></b></span><span>认证失败 <b><AnimatedNumber :value="totals.failure"/></b></span></div><div class="range-switch" aria-label="趋势时间范围"><button v-for="(text,value) in {'24h':'24小时','7d':'7天','14d':'14天','30d':'30天'}" :key="value" :aria-pressed="range===value" :class="{selected:range===value}" @click="hideTip();emit('range',value)">{{text}}</button></div></div>
 <div class="trend-legend"><span><i class="trend-success-dot"/>成功</span><span><i class="trend-failure-dot"/>失败</span><small v-if="range!=='24h'">按 UTC 日汇总</small></div>
 <div class="echarts-trend" tabindex="0" role="group" :aria-label="`登录趋势，成功 ${totals.success} 次，失败 ${totals.failure} 次。左右方向键查看明细，Esc 关闭。`" @keydown.left.prevent="move(-1)" @keydown.right.prevent="move(1)" @keydown.esc.prevent="hideTip" @blur="hideTip" @pointerleave="hideTip"><div ref="host" class="echarts-canvas"/></div>
 </section>
</template>
