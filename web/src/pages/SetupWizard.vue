<script setup lang="ts">
import { computed, onUnmounted, reactive, ref, watch } from 'vue';

const emit = defineEmits<{ complete: [credentials: {username:string;password:string}] }>();
const storageKey = 'sshguard.setup-token';
const linkToken = new URLSearchParams(location.hash.slice(1)).get('setup') || '';
let savedToken = '';
try {
  savedToken = sessionStorage.getItem(storageKey) || '';
  if (linkToken) sessionStorage.setItem(storageKey, linkToken);
} catch {}
const token = ref(linkToken || savedToken);
if (linkToken) history.replaceState(null, '', location.pathname + location.search);
const hasLinkToken = Boolean(token.value);
const form = reactive({driver:'sqlite',host:'127.0.0.1',port:3306,database:'sshguard',db_username:'sshguard',db_password:'',tls:false,username:'admin',password:''});
const confirmation = ref(''), busy = ref(false), error = ref(''), tested = ref(false), completed = ref(false), showPassword = ref(false);
const advanced = ref(false), tokenExpanded = ref(!hasLinkToken);
const endpoint = location.origin;
const insecure = location.protocol === 'http:';
const databaseName = computed(() => ({sqlite:'SQLite · 本机存储',mysql:'MySQL · 外部数据库',postgres:'PostgreSQL · 外部数据库'}[form.driver]));
const bytes = (value:string) => new TextEncoder().encode(value).length;
let active = true;
onUnmounted(() => { active = false; });
watch(() => [form.driver, form.host, form.port, form.database, form.db_username, form.db_password, form.tls, token.value], () => { tested.value = false; });
function selectDriver() { form.port = form.driver === 'postgres' ? 5432 : 3306; }

async function request(action:string) {
  const {username, password, ...database} = form;
  const response = await fetch('/api/v1/setup/'+action, {
    method:'POST',headers:{'Content-Type':'application/json','X-Setup-Token':token.value.trim()},
    body:JSON.stringify(action === 'test' ? database : {...database,username:username.trim(),password}),
    signal:AbortSignal.timeout(65000)
  });
  const result = await response.json().catch(() => ({}));
  if (!response.ok) throw Error(result.error || '初始化失败，请稍后重试');
}
function markCompleted() {
  completed.value = true; form.db_password = ''; token.value = '';
  try { sessionStorage.removeItem(storageKey); } catch {}
}
async function run(finish:boolean) {
  busy.value = true; error.value = '';
  try {
    if (!token.value.trim()) { tokenExpanded.value = true; throw Error('请使用安装终端中的初始化链接，或填写初始化口令。'); }
    if (finish) {
      if (bytes(form.username.trim()) < 3 || bytes(form.username.trim()) > 64) throw Error('用户名需为 3–64 字节。');
      if (bytes(form.password) < 12 || bytes(form.password) > 72) throw Error('密码需为 12–72 字节，中文字符通常占 3 字节。');
      if (form.password !== confirmation.value) throw Error('两次输入的密码不一致。');
    }
    await request(finish ? 'complete' : 'test');
    if (!finish) { tested.value = true; return; }
    markCompleted();
    await waitReady();
  } catch (e) {
    // A proxy timeout or lost response may happen after the database commit.
    // Check the server state before inviting the user to initialize again.
    if (finish && !completed.value) {
      try {
        const response = await fetch('/api/v1/setup/status',{signal:AbortSignal.timeout(5000)});
        const result = await response.json();
        if (response.ok && result.data?.required === false) {
          markCompleted(); await waitReady(); return;
        }
      } catch {}
    }
    error.value = e instanceof Error && e.name === 'TimeoutError' ? '请求超时，请检查连接后重试；已保存的配置会保留。' : (e as Error).message;
    if (error.value.includes('口令')) tokenExpanded.value = true;
  } finally { busy.value = false; }
}
async function waitReady() {
  for (let i=0;i<30 && active;i++) {
    await new Promise(resolve => setTimeout(resolve,1000));
    try {
      const response = await fetch('/api/v1/health',{signal:AbortSignal.timeout(3000)});
      const result = await response.json();
      if (active && response.ok && result.data?.status === 'ok') {
        const credentials = {username:form.username.trim(),password:form.password};
        form.password = ''; confirmation.value = '';
        emit('complete',credentials); return;
      }
    } catch {}
  }
  if (active) throw Error('账号已创建，控制台仍在启动。可重新检查，或稍后刷新并使用新账号登录。');
}
async function retryReady() {
  busy.value = true; error.value = '';
  try { await waitReady(); } catch (e) { error.value = (e as Error).message; } finally { busy.value = false; }
}
</script>

<template>
  <main class="setup-screen">
    <div class="setup-brand"><span class="brand-mark" aria-hidden="true">S</span> SSHGuard <span class="brand-caption">服务器登录审计</span></div>
    <div class="setup-layout">
      <div class="setup-intro">
        <div class="ready-badge"><span></span> 主控服务已启动</div>
        <h1>从这里开始，<br>掌握每一次登录。</h1>
        <p class="setup-description">创建一个管理员账号，即可集中查看服务器登录、追踪异常并接收告警。</p>
        <ol class="setup-steps" aria-label="安装进度">
          <li class="done"><span class="step-number" aria-hidden="true">✓</span><div><b>安装主控</b><small>程序和存储目录已就绪</small></div></li>
          <li :class="completed ? 'done' : 'current'" :aria-current="!completed ? 'step' : undefined"><span class="step-number" aria-hidden="true">{{completed ? '✓' : '2'}}</span><div><b>创建管理员</b><small>设置你的控制台登录账号</small></div></li>
          <li :class="{current:completed}" :aria-current="completed ? 'step' : undefined"><span class="step-number" aria-hidden="true">3</span><div><b>接入服务器</b><small>复制命令，等待 Agent 上线</small></div></li>
        </ol>
        <div class="connection-info"><small>当前主控地址</small><code>{{endpoint}}</code><p>IP 和端口即可访问，随时可以配置域名和 HTTPS。</p></div>
      </div>
      <section class="initialization-card" aria-labelledby="setup-title" :aria-busy="busy">
        <template v-if="completed">
          <div class="completion-mark" aria-hidden="true">✓</div>
          <div class="eyebrow">初始化已完成</div><h2 id="setup-title">你的主控已准备就绪</h2>
          <p class="form-description" role="status">管理员已创建，{{busy ? '正在进入控制台…' : '等待控制台启动。'}}</p>
          <div class="next-step"><b>接下来，添加第一台服务器</b><p>选择操作系统，复制安装命令。Agent 上线后，登录事件会自动显示在控制台。</p></div>
          <button v-if="!busy" class="primary" type="button" @click="retryReady">重新检查并进入控制台 <span aria-hidden="true">→</span></button>
        </template>
        <template v-else>
          <div class="eyebrow">欢迎使用 SSHGUARD</div><h2 id="setup-title">创建管理员账号</h2>
          <p class="form-description">只差这一步，就能开始接入服务器。</p>
          <form @submit.prevent="run(true)">
            <fieldset :disabled="busy" class="account-fields"><legend class="sr-only">管理员登录信息</legend>
              <label for="setup-username">用户名</label><input id="setup-username" v-model="form.username" minlength="3" maxlength="64" autocomplete="username" required />
              <div class="password-label"><label for="setup-password">密码</label><button type="button" class="text-button" :aria-pressed="showPassword" @click="showPassword=!showPassword">{{showPassword ? '隐藏密码' : '显示密码'}}</button></div>
              <input id="setup-password" v-model="form.password" :type="showPassword ? 'text' : 'password'" minlength="12" maxlength="72" autocomplete="new-password" aria-describedby="password-hint" placeholder="至少 12 位，建议包含字母和数字" required />
              <small id="password-hint" class="field-hint">12–72 字节。请保存好，后续用于登录控制台。</small>
              <label for="setup-confirmation">确认密码</label><input id="setup-confirmation" v-model="confirmation" :type="showPassword ? 'text' : 'password'" autocomplete="new-password" placeholder="再次输入密码" required />
            </fieldset>
            <div class="identity-status" :class="{missing:!hasLinkToken}"><span aria-hidden="true">{{hasLinkToken ? '✓' : '!'}}</span><span>{{hasLinkToken ? '已从安装链接读取初始化口令' : '需要安装终端提供的初始化口令'}}</span><button class="text-button" type="button" :aria-expanded="tokenExpanded" :disabled="busy" @click="tokenExpanded=!tokenExpanded">{{tokenExpanded ? '收起' : hasLinkToken ? '修改' : '填写'}}</button></div>
            <div v-if="tokenExpanded" class="token-field"><label for="setup-token">初始化口令</label><input id="setup-token" v-model="token" :disabled="busy" type="password" autocomplete="off" :required="!hasLinkToken" /><small class="field-hint">在主控终端执行 <code>sudo sh /opt/SSHGuard/Server/sshguard.sh --setup-link</code>，使用显示的链接重新打开此页。</small></div>
            <details :open="advanced" class="advanced-settings" @toggle="advanced=($event.target as HTMLDetailsElement).open">
              <summary><span>存储设置 <small>可选</small></span><span class="database-summary">{{databaseName}}</span></summary>
              <fieldset :disabled="busy" class="database-fields"><legend class="sr-only">数据库设置</legend>
                <p class="field-hint">默认使用本机 SQLite，无需安装数据库服务。</p>
                <label for="setup-driver">数据库类型</label><select id="setup-driver" v-model="form.driver" @change="selectDriver"><option value="sqlite">SQLite（推荐）</option><option value="mysql">MySQL</option><option value="postgres">PostgreSQL</option></select>
                <template v-if="form.driver!=='sqlite'">
                  <p class="field-hint">请提前创建专用空数据库和账号，已有数据不会被覆盖。</p>
                  <div class="field-row"><div><label for="db-host">主机</label><input id="db-host" v-model="form.host" required /></div><div class="port-field"><label for="db-port">端口</label><input id="db-port" v-model.number="form.port" type="number" min="1" max="65535" required /></div></div>
                  <label for="db-name">数据库名</label><input id="db-name" v-model="form.database" required />
                  <label for="db-username">数据库用户名</label><input id="db-username" v-model="form.db_username" autocomplete="off" required />
                  <label for="db-password">数据库密码</label><input id="db-password" v-model="form.db_password" type="password" autocomplete="new-password" />
                  <label class="check"><input v-model="form.tls" type="checkbox" />使用 TLS 并校验证书</label>
                </template>
                <div class="test-connection"><button type="button" :disabled="busy||!token.trim()" @click="run(false)">{{busy ? '正在处理…' : '测试数据库连接'}}</button><span v-if="tested" class="test-success" role="status">✓ 连接成功</span></div>
              </fieldset>
            </details>
            <p v-if="!advanced" class="storage-note">{{form.driver==='sqlite' ? '数据将保存在当前服务器，后续升级会保留。' : '将使用上方所选的外部数据库。'}}</p>
            <p v-if="error" class="setup-error" role="alert">{{error}}</p>
            <button class="primary" :disabled="busy" type="submit">{{busy ? '正在创建并启动…' : '创建账号并进入控制台'}} <span v-if="!busy" aria-hidden="true">→</span></button>
            <p class="submit-hint">初始化仅需完成一次。</p>
          </form>
        </template>
        <p v-if="completed && error" class="setup-error" role="alert">{{error}}</p>
      </section>
    </div>
    <p class="setup-footer">{{insecure ? '当前通过 HTTP 直连，登录与 Agent 传输未加密。可按需启用 HTTPS。' : '当前通过 HTTPS 连接，登录与 Agent 传输使用加密连接。'}}</p>
  </main>
</template>

<style scoped>
.setup-screen{min-height:100vh;box-sizing:border-box;padding:36px 28px 24px;background:#f5f7fb;color:#17243c;font-family:Inter,-apple-system,BlinkMacSystemFont,"Segoe UI","Microsoft YaHei",sans-serif}
.setup-brand{display:flex;align-items:center;gap:10px;max-width:1060px;margin:0 auto 28px;font-size:18px;font-weight:750;letter-spacing:-.4px}
.brand-mark{display:grid;place-items:center;width:31px;height:35px;background:#285ee8;color:#fff;border-radius:8px 8px 12px 12px;font-size:21px}
.brand-caption{font-size:12px;font-weight:400;letter-spacing:0;color:#788397;margin-left:8px;border-left:1px solid #dbe1eb;padding-left:18px}
.setup-layout{display:grid;grid-template-columns:360px minmax(0,1fr);max-width:1060px;margin:auto;border:1px solid #e0e6ef;border-radius:18px;overflow:hidden;background:white;box-shadow:0 16px 48px #14284b07}
.setup-intro{padding:39px 34px;background:#14243e;color:white;display:flex;flex-direction:column}
.ready-badge{display:flex;align-items:center;gap:8px;align-self:flex-start;font-size:12px;color:#c1e4d8;background:#ffffff0a;border:1px solid #ffffff12;border-radius:20px;padding:7px 11px}
.ready-badge>span{width:6px;height:6px;border-radius:50%;background:#60cea4}
.setup-intro h1{font-size:29px;line-height:1.55;letter-spacing:-1px;margin:27px 0 14px;font-weight:650}
.setup-description{font-size:13px;line-height:1.9;color:#a9b7ce;margin:0}
.setup-steps{list-style:none;margin:37px 0 40px;padding:0}
.setup-steps li{display:flex;gap:14px;align-items:flex-start;position:relative;padding-bottom:29px;color:#8a9bb6}
.setup-steps li:last-child{padding-bottom:0}
.setup-steps li:not(:last-child):after{content:"";position:absolute;left:14px;top:35px;bottom:6px;width:1px;background:#ffffff1a}
.step-number{display:grid;place-items:center;flex:0 0 29px;height:29px;border:1px solid #ffffff24;border-radius:50%;box-sizing:border-box;font-size:12px}
.setup-steps b{display:block;font-size:14px;line-height:29px;font-weight:550}
.setup-steps small{display:block;font-size:12px;line-height:1.7;color:#8596b1}
.setup-steps .done .step-number{color:#69d4b0;background:#1b413f;border-color:#2c5d54}
.setup-steps .current{color:#fff}
.setup-steps .current .step-number{background:#3973ef;border-color:#3973ef}
.setup-steps .current small{color:#aebcd1}
.connection-info{margin-top:auto;border-top:1px solid #ffffff17;padding-top:22px}
.connection-info>small{display:block;color:#90a2be;font-size:11px}
.connection-info>code{display:block;overflow-wrap:anywhere;font-size:13px;color:#dce6f7;margin:10px 0}
.connection-info>p{font-size:11px;line-height:1.8;color:#8b9cb7;margin:0}
.initialization-card{padding:37px 48px 28px;min-width:0}
.eyebrow{font-size:10px;font-weight:650;letter-spacing:1.5px;color:#7d8aa0}
.initialization-card h2{font-size:25px;letter-spacing:-.6px;font-weight:650;margin:12px 0 8px}
.form-description{font-size:13px;color:#7b879b;line-height:1.8;margin:0 0 23px}
.initialization-card form{margin:0}
.initialization-card fieldset{border:0;padding:0;margin:0;min-width:0}
.initialization-card label{display:block;font-size:12px;font-weight:550;margin:17px 0 8px}
.account-fields>label:first-of-type{margin-top:0}
.initialization-card input:not([type=checkbox]),.initialization-card select{width:100%;box-sizing:border-box;border:1px solid #d8e0eb;background:#fff;color:#1d2d46;border-radius:7px;font:inherit;font-size:13px;padding:11px 12px;min-height:41px}
.initialization-card input::placeholder{color:#a0abba;font-size:12px}
.password-label{display:flex;align-items:center;justify-content:space-between;margin-top:17px;margin-bottom:8px}
.password-label label{margin:0}
.initialization-card button{font:inherit;font-size:12px;cursor:pointer;border:1px solid #d8e0eb;border-radius:7px;background:white;padding:9px 13px;color:#41536f}
.initialization-card .text-button{border:0;background:none;color:#3968cf;padding:0;white-space:nowrap;font-size:11px}
.field-hint{display:block;color:#8390a4;font-size:11px;line-height:1.8;margin:8px 0 0;overflow-wrap:anywhere}
.identity-status{display:flex;align-items:center;gap:7px;margin-top:24px;font-size:11px;color:#5b796f}
.identity-status>span:first-child{color:#22936e}
.identity-status button{margin-left:auto}
.identity-status.missing,.identity-status.missing>span:first-child{color:#a47332}
.token-field{margin:12px 0 20px}
.token-field code{font-size:10px;overflow-wrap:anywhere}
.advanced-settings{margin-top:20px;border-top:1px solid #e8edf4;border-bottom:1px solid #e8edf4}
.advanced-settings summary{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:16px 0;cursor:pointer;list-style:none;font-size:12px}
.advanced-settings summary::-webkit-details-marker{display:none}
.advanced-settings summary>span:first-child:before{content:"›";display:inline-block;margin-right:9px;color:#8693a7;transition:transform .15s}
.advanced-settings[open] summary>span:first-child:before{transform:rotate(90deg)}
.advanced-settings summary small{font-size:10px;color:#95a0b2;margin-left:5px}
.database-summary{font-size:10px;color:#718197}
.database-fields{padding:0 0 19px!important}
.database-fields>.field-hint:first-child{margin-top:0}
.field-row{display:flex;gap:12px}.field-row>div{flex:1;min-width:0}.field-row .port-field{flex:0 0 96px}
.initialization-card .check{display:flex;align-items:center;gap:7px;font-size:11px;font-weight:400;color:#687b95}
.check input{margin:0;accent-color:#285ee8}
.test-connection{display:flex;gap:12px;align-items:center;margin-top:17px}
.test-success{font-size:11px;color:#208765}
.storage-note{font-size:10px;color:#8b97aa;margin:10px 0 0;line-height:1.8}
.initialization-card .primary{display:flex;align-items:center;justify-content:center;gap:14px;width:100%;padding:13px 16px;margin-top:24px;background:#285ee8;border-color:#285ee8;color:white;font-size:13px;font-weight:550;box-shadow:0 4px 8px #285ee81a}
.primary:hover{background:#1e50d1}
.submit-hint{text-align:center;font-size:10px;color:#9aa5b6;margin:12px 0 0}
.setup-error{padding:11px 13px;border:1px solid #f5d4ce;border-radius:7px;background:#fff5f3;color:#ae3e30;font-size:12px;line-height:1.8;margin:17px 0 0;overflow-wrap:anywhere}
.initialization-card button:disabled{opacity:.6;cursor:wait}
.initialization-card input:focus,.initialization-card select:focus,.initialization-card button:focus-visible,summary:focus-visible{outline:2px solid #3973ef;outline-offset:2px}
.setup-footer{max-width:1060px;text-align:center;font-size:11px;line-height:1.8;color:#8b97aa;margin:20px auto 0}
.completion-mark{display:grid;place-items:center;width:52px;height:52px;background:#e8f6ef;color:#238866;border-radius:50%;font-size:26px;margin:32px 0 26px}
.next-step{border:1px solid #e1e8f4;background:#f7f9fd;border-radius:9px;padding:18px;margin:30px 0}
.next-step b{font-size:13px}.next-step p{font-size:12px;line-height:1.9;color:#798aa2;margin:8px 0 0}
.sr-only{position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0,0,0,0);white-space:nowrap}
@media(min-width:1400px){.setup-screen{padding-top:56px}}
@media(max-width:800px){.setup-layout{grid-template-columns:270px minmax(0,1fr)}.setup-intro{padding:30px 24px}.setup-intro h1{font-size:24px}.initialization-card{padding:32px 28px}}
@media(max-width:640px){.setup-screen{padding:22px 16px}.setup-brand{margin-bottom:20px}.setup-layout{grid-template-columns:1fr}.setup-intro{padding:22px 24px}.ready-badge{font-size:11px}.setup-intro h1{font-size:22px;margin:16px 0 0}.setup-intro h1 br{display:none}.setup-description,.connection-info{display:none}.setup-steps{display:flex;justify-content:space-between;margin:22px 0 0;gap:8px}.setup-steps li{gap:7px;padding:0;align-items:center}.setup-steps li:after,.setup-steps small{display:none}.setup-steps b{font-size:11px}.step-number{flex-basis:24px;height:24px;font-size:10px}.initialization-card{padding:28px 24px}.initialization-card h2{font-size:23px}.brand-caption{font-size:11px}.setup-footer{font-size:10px}.database-summary{font-size:9px}}
@media(prefers-reduced-motion:reduce){*{transition:none!important}}
</style>
