<script setup lang="ts">
import { computed, reactive, watch, type Component } from 'vue';
import { ElIcon } from 'element-plus';
import { Lock, View, Bell, Setting, User, ArrowDown } from '@element-plus/icons-vue';

const props = defineProps<{
  items: {id: string; title: string; icon: Component}[];
  active: string;
  version?: string;
}>();
const emit = defineEmits<{navigate: [id: string]}>();
const categories = [
  {id: 'monitoring', title: '安全监控', icon: View, pages: ['events', 'rules', 'ip-allowlist']},
  {id: 'notifications', title: '告警与通知', icon: Bell, pages: ['alerts', 'channels', 'notifications']},
  {id: 'system', title: '系统设置', icon: Setting, pages: ['geoip', 'database']},
  {id: 'security', title: '账号与安全', icon: User, pages: ['users', 'login-protection', 'audit-logs']},
];
const groups = computed(() => categories.map(group => ({
  ...group,
  items: group.pages.flatMap(id => props.items.filter(item => item.id === id)),
})).filter(group => group.items.length));
const directItems = computed(() => props.items.filter(item => !categories.some(group => group.pages.includes(item.id))));
const expanded = reactive<Record<string, boolean>>({});
watch(() => [props.active, groups.value] as const, () => {
  const group = groups.value.find(group => group.pages.includes(props.active));
  if (group) expanded[group.id] = true;
}, {immediate: true});
</script>

<template>
  <div class="console-navigation">
    <div class="sidebar-heading">
      <div class="brand"><el-icon><Lock/></el-icon> SSHGuard</div>
      <slot/>
    </div>
    <nav aria-label="主导航">
      <button v-for="item in directItems" :key="item.id" type="button"
        :class="{selected: active === item.id}"
        :aria-current="active === item.id ? 'page' : undefined"
        @click="emit('navigate', item.id)">
        <el-icon :class="['nav-icon', 'nav-icon-' + item.id]"><component :is="item.icon" :key="item.id"/></el-icon>
        {{ item.title }}
      </button>
      <div v-for="group in groups" :key="group.id" class="nav-group" :class="{'is-expanded': expanded[group.id]}">
        <button type="button" class="nav-group-toggle"
          :class="{'group-active': group.pages.includes(active)}"
          :aria-expanded="!!expanded[group.id]" :aria-controls="'nav-group-' + group.id"
          @click="expanded[group.id] = !expanded[group.id]">
          <el-icon :class="['nav-group-icon', 'nav-group-icon-' + group.id]"><component :is="group.icon"/></el-icon>
          <span>{{ group.title }}</span>
          <el-icon class="nav-group-chevron" :class="{expanded: expanded[group.id]}"><ArrowDown/></el-icon>
        </button>
        <div :id="'nav-group-' + group.id" class="nav-group-collapse" :inert="!expanded[group.id]" :aria-hidden="!expanded[group.id]">
         <div class="nav-group-clip"><div class="nav-group-items">
          <button v-for="item in group.items" :key="item.id" type="button"
            :class="{selected: active === item.id}"
            :aria-current="active === item.id ? 'page' : undefined"
            @click="emit('navigate', item.id)">
            <el-icon :class="['nav-icon', 'nav-icon-' + item.id]"><component :is="item.icon"/></el-icon>
            {{ item.title }}
          </button>
         </div></div>
        </div>
      </div>
    </nav>
    <div class="sidebar-bottom"><span class="dot"></span> {{ version || 'SSHGuard' }}</div>
  </div>
</template>
