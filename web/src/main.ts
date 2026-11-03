import { createApp } from 'vue';
import { ElMessage, ElMessageBox, provideGlobalConfig } from 'element-plus';
import zhCn from 'element-plus/es/locale/lang/zh-cn';
import App from './App.vue';
import './styles/console.css';
import 'element-plus/theme-chalk/dark/css-vars.css';
import './styles/theme.css';
import './styles/dialogs.css';
import './styles/toolbar.css';
import './styles/navigation.css';
import './lib/theme';
const app = createApp(App);
provideGlobalConfig({locale:zhCn},app,true);
app.use(ElMessage).use(ElMessageBox).mount('#app');

import './styles/login.css';
