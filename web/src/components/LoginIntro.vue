<template>
 <section class="login-visual">
  <div class="visual-grid" aria-hidden="true"/>
  <div class="visual-brand"><span class="brand-mark"><svg viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M7 10V7a5 5 0 0 1 10 0v3M6 10h12v11H6zM12 14v3" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"/></svg></span><b>SSHGuard</b><span class="brand-caption">安全控制台</span></div>
  <div class="visual-content">
   <div class="visual-kicker"><span/> CONNECTIONS, ACCOUNTED FOR</div>
   <h1>每一次连接，<br>都有<span>迹可循。</span></h1>
   <p>从终端到远程桌面，<br class="mobile-break">让服务器的每一次登录清晰可见。</p>
   <div class="connection-scene" role="img" aria-label="SSH、RDP 和 VNC 登录汇入统一审计的示意图">
    <svg class="connection-lines" viewBox="0 0 540 260" fill="none" aria-hidden="true">
     <defs><linearGradient id="login-route"><stop stop-color="#4776b6"/><stop offset="1" stop-color="#81c9f5"/></linearGradient></defs>
     <path class="route" d="M90 55H170Q190 55 190 75V110Q190 130 210 130H270M90 130H270M90 205H170Q190 205 190 185V150Q190 130 210 130H270M325 130H450"/>
     <path class="packet packet-one" d="M90 55H170Q190 55 190 75V110Q190 130 210 130H270"/>
     <path class="packet packet-two" d="M90 130H270"/>
     <path class="packet packet-three" d="M90 205H170Q190 205 190 185V150Q190 130 210 130H270"/>
     <circle class="orbit" cx="297" cy="130" r="57"/><circle class="orbit outer" cx="297" cy="130" r="74"/>
    </svg>
    <div class="protocol-node ssh"><span>⌘</span><b>SSH</b><i/></div>
    <div class="protocol-node rdp"><svg viewBox="0 0 20 20" fill="none" aria-hidden="true"><rect x="2" y="3" width="16" height="11" rx="2"/><path d="M6 18h8M10 14v4"/></svg><b>RDP</b><i/></div>
    <div class="protocol-node vnc"><svg viewBox="0 0 20 20" fill="none" aria-hidden="true"><rect x="2" y="2" width="16" height="16" rx="3"/><path d="M6 7h8M6 10h5M6 13h3"/></svg><b>VNC</b><i/></div>
    <div class="security-core"><svg viewBox="0 0 40 44" fill="none" aria-hidden="true"><path d="M20 3 35 9v12c0 10-15 20-15 20S5 31 5 21V9L20 3Z"/><path class="shield-check" d="m13 21 5 5 10-11"/></svg></div>
    <div class="audit-node"><span>✓</span><b>统一审计</b><small>记录 · 追溯 · 告警</small></div>
    <span class="scene-caption">多协议登录审计示意</span>
   </div>
   <div class="visual-features"><span><i/>多协议采集</span><span><i/>实时告警</span><span><i/>自主部署</span></div>
  </div>
  <footer class="visual-footer"><span>你的服务器。你的安全视图。</span></footer>
 </section>
</template>

<style scoped>
.login-visual{--ink:#e9f1ff;position:relative;isolation:isolate;display:flex;flex-direction:column;min-width:0;overflow:hidden;padding:44px clamp(30px,5.5vw,130px);color:var(--ink);background:radial-gradient(ellipse at 75% 65%,#18375b88,transparent 52%),radial-gradient(ellipse at 0 100%,#30366588,transparent 60%),#101c30}
.visual-grid{position:absolute;inset:0;z-index:-1;background-image:linear-gradient(#99bbdf08 1px,transparent 1px),linear-gradient(90deg,#99bbdf08 1px,transparent 1px);background-size:52px 52px;mask-image:linear-gradient(135deg,transparent,#000,transparent);pointer-events:none}
.login-visual:after{content:'';position:absolute;width:440px;height:440px;border-radius:50%;border:1px solid #8db8ef0c;top:-280px;right:-150px;box-shadow:0 0 0 60px #8db8ef03,0 0 0 120px #8db8ef03;z-index:-1}
.visual-brand{display:flex;align-items:center;gap:12px;letter-spacing:-.6px;font-size:24px}.brand-mark{display:grid;place-items:center;width:37px;height:42px;background:#4182f4;border-radius:11px;box-shadow:0 5px 22px #2575f530}.brand-mark svg{width:21px;height:24px}.brand-caption{font-size:10px;font-weight:400;letter-spacing:2px;color:#93a8c6;margin-left:4px;padding-left:16px;border-left:1px solid #426080}
.visual-content{width:100%;max-width:590px;margin:auto 0;padding:64px 0 44px;animation:visual-in .8s ease both}.visual-kicker{display:flex;align-items:center;gap:9px;color:#8eb6e7;font-size:10px;letter-spacing:2px}.visual-kicker>span{height:5px;width:5px;background:#77b6fb;border-radius:50%;box-shadow:0 0 14px #6eafff}
h1{font-size:clamp(36px,3.3vw,66px);line-height:1.35;letter-spacing:-2px;font-weight:600;margin:25px 0 20px;color:var(--ink)}h1 span{background:linear-gradient(90deg,#8cbfff,#b5d4fa 50%,#a3aaf3);background-clip:text;-webkit-text-fill-color:transparent}.visual-content>p{font-size:13px;line-height:1.9;color:#9eafc8;margin:0}.mobile-break{display:none}
.connection-scene{position:relative;width:100%;aspect-ratio:540/260;max-width:540px;margin:30px 0 17px;user-select:none}.connection-lines{position:absolute;inset:0;width:100%;height:100%;overflow:visible}.route{stroke:url(#login-route);stroke-width:1;opacity:.45}.packet{stroke:#a8d9ff;stroke-width:2;stroke-linecap:round;stroke-dasharray:12 290;stroke-dashoffset:302;animation:packet-flow 5s linear infinite}.packet-two{animation-delay:1.6s}.packet-three{animation-delay:3.1s}.orbit{stroke:#79b6f6;stroke-width:.7;stroke-dasharray:2 6;opacity:.25;transform-origin:297px 130px;animation:orbit-turn 60s linear infinite}.orbit.outer{stroke-dasharray:none;opacity:.1;animation:none}
.protocol-node{position:absolute;left:0;width:24%;height:39px;display:flex;align-items:center;gap:11px;padding:0 13px;border:1px solid #4d719542;background:linear-gradient(120deg,#22354c,#172940);border-radius:9px;box-shadow:0 6px 24px #030c181a;color:#c9d8ed;font-size:11px;transform:translateY(-50%)}.protocol-node.ssh{top:21.15%}.protocol-node.rdp{top:50%}.protocol-node.vnc{top:78.85%}.protocol-node>span{font-size:20px;color:#86b5ed}.protocol-node svg{width:18px;height:18px;stroke:#86b5ed;stroke-width:1.2}.protocol-node b{letter-spacing:1px;font-weight:500}.protocol-node i{width:4px;height:4px;border-radius:50%;background:#70cab1;margin-left:auto}
.security-core{position:absolute;left:55%;top:50%;transform:translate(-50%,-50%);width:70px;height:78px;border-radius:20px;display:grid;place-items:center;background:linear-gradient(140deg,#345784,#1b344f);border:1px solid #719bd35c;box-shadow:0 0 45px #448df42b,inset 0 1px #d6e7ff1a}.security-core svg{width:35px;height:40px;stroke:#b6d6ff;stroke-width:1.5}.shield-check{stroke:#9fe6d3;stroke-width:2;stroke-linecap:round;stroke-linejoin:round}
.audit-node{position:absolute;right:0;top:50%;transform:translateY(-50%);width:23%;text-align:center;padding:15px 3px;border:1px solid #5988ae33;border-radius:12px;background:#152a40}.audit-node>span{display:block;color:#8bdbc3;font-size:19px;margin-bottom:7px}.audit-node b{display:block;font-size:11px;font-weight:500;color:#c6d8ef}.audit-node small{display:block;color:#7f9bbd;font-size:8px;margin-top:6px}.scene-caption{position:absolute;bottom:0;left:0;font-size:9px;color:#7188a7;letter-spacing:1px}
.visual-features{display:flex;gap:22px;border-top:1px solid #6b8bb624;padding-top:23px;color:#a2b4cc;font-size:11px}.visual-features span{display:flex;align-items:center;gap:7px}.visual-features i{width:4px;height:4px;background:#688bb8;transform:rotate(45deg)}.visual-footer{display:flex;align-items:center;justify-content:space-between;gap:10px;color:#7d92b0;font-size:10px}
@keyframes packet-flow{to{stroke-dashoffset:0}}@keyframes orbit-turn{to{transform:rotate(360deg)}}@keyframes visual-in{from{opacity:0;transform:translateY(14px)}to{opacity:1;transform:translateY(0)}}
@media(min-width:1900px){.visual-content{max-width:640px}.connection-scene{max-width:600px;margin-top:42px}.visual-content>p{font-size:15px}.visual-features{font-size:12px}}
@media(max-width:1000px){.login-visual{padding:34px}.visual-content{padding:52px 0 35px}h1{font-size:38px}.protocol-node{gap:6px;padding:0 8px;height:34px}.security-core{width:54px;height:62px;border-radius:16px}.audit-node small{font-size:7px}.visual-features{gap:15px;font-size:10px}.visual-brand{font-size:22px}.brand-caption{font-size:8px;letter-spacing:1px;padding-left:10px}}
@media(max-width:700px){.login-visual{display:none}.visual-content{padding:32px 0 0;margin:0;max-width:none}.visual-kicker{font-size:8px;letter-spacing:1.5px}h1{font-size:34px;line-height:1.25;margin:16px 0 12px}.visual-content>p{font-size:11px}.connection-scene,.visual-features,.visual-footer{display:none}.visual-brand{font-size:21px}.brand-mark{width:31px;height:35px}.brand-caption{display:none}.mobile-break{display:initial}}
@media(prefers-reduced-motion:reduce){*,*:before,*:after{animation:none!important;transition:none!important}}
</style>
