type LoginEvent = {event_type?:string;username?:string;source_ip?:string;source_port?:number};
export const loginProtocol=(event:LoginEvent)=>(event.event_type||'ssh.login').split('.')[0];
export const loginLabel=(event:LoginEvent)=>({ssh:'SSH 登录',rdp:'RDP 远程桌面',vnc:'VNC 登录',console:'远程 VNC 登录'}[loginProtocol(event)]||'登录事件');
export const loginUser=(event:LoginEvent)=>loginProtocol(event)==='console'?'不记录用户名':event.username||'未提供';
export const loginSource=(event:LoginEvent)=>!event.source_ip?'不可获取':event.source_port?`${event.source_ip.includes(':')?'['+event.source_ip+']':event.source_ip}:${event.source_port}`:event.source_ip;
export const consoleHint='有人通过服务器控制台登录系统，例如服务商网页上的 VNC / KVM，也可能是在服务器本机直接登录。此类记录不包含用户名，无法确认是否来自远程连接。';
