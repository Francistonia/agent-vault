import fs from 'node:fs/promises';
import {fileURLToPath} from 'node:url';

const ROOT=fileURLToPath(new URL('../../',import.meta.url)).replaceAll('\\','/').replace(/\/$/,'');
const C={dark:'#0B1117',white:'#F3F6F5',mint:'#76E6BA',ink:'#14221E',sub:'#A6B5B1',gray:'#53655E',rust:'#B24F37'};

export async function buildDeck(p){
  p.theme.defaultFont='Microsoft YaHei';
  const assets={};
  for(const [key,path] of Object.entries({cover:'docs/presentation/assets/vault-cover.png',ssh:'.test-data/screenshots/product-ssh-detail.png',web:'.test-data/screenshots/product-password-login.png',product:'.test-data/screenshots/product-dark.png'}))assets[key]=new Uint8Array(await fs.readFile(`${ROOT}/${path}`));
  let counter=0;
  const text=(s,copy,x,y,w,h,size=28,color=C.white,bold=false)=>{
    const shape=s.shapes.add({geometry:'textbox',name:`copy-${++counter}`,position:{left:x,top:y,width:w,height:h},fill:'none',line:{fill:'none',width:0},text:{text:copy,style:{fontSize:size,color,bold,typeface:'Microsoft YaHei',insets:0,autoFit:'none',verticalAlignment:'top',wrap:'square'}}});
    return shape;
  };
  const image=(s,key,x,y,w,h,extra={})=>s.images.add({blob:assets[key],contentType:'image/png',alt:key==='cover'?'保险库概念主视觉':`Agent Vault 真实测试界面：${key}`,fit:'contain',position:{left:x,top:y,width:w,height:h},...extra});
  const slide=(fill,notes)=>{const s=p.slides.add({width:1280,height:720,background:{fill}});s.speakerNotes.text=notes;return s;};
  const title=(s,copy,color=C.white)=>text(s,copy,72,62,1136,108,46,color,true);
  const source='产品事实依据：C:/workspace/safe/README.md；docs/implementation-status.md；internal/vault/model.go；internal/vault/host.go。当前交付版本 0.1.7，2026-10-23。';
  let s;

  s=slide(C.dark,source+' 封面为生成的保险库概念插画，不是实物或产品截图。');
  image(s,'cover',0,0,1280,720,{fit:'cover'});
  text(s,'Agent Vault',72,78,650,68,48,C.white,true);
  text(s,'让 Agent 登录\n凭据留在本机',72,243,670,190,66,C.white,true);
  text(s,'Windows 上的本地凭据工具',76,529,590,42,26,C.mint);

  s=slide(C.white,source+' 这是风险路径说明，不宣称每个工具必然泄露，也不宣称产品覆盖任意工具。');
  title(s,'登录任务中的凭据暴露路径',C.ink);
  text(s,'“登录服务器，\n看看磁盘情况”',72,222,680,200,57,C.ink,true);
  text(s,'一句自然语言任务\n可能涉及多处凭据处理',76,499,660,92,30,C.gray);
  text(s,'提示词',854,230,360,65,42,C.rust,true);
  text(s,'脚本与命令参数',854,326,360,60,32,C.ink);
  text(s,'工具返回与记录',854,414,360,60,32,C.ink);
  text(s,'密码应由可信执行端使用',854,557,360,75,25,C.gray);

  s=slide(C.dark,source+' 数据库驱动支持 PostgreSQL / MySQL；SSH 当前只支持 Ed25519 私钥；网页登录有三种本机配置模式。');
  title(s,'支持三种真实工作场景');
  for(const [n,label,sub,y] of [
    ['SSH','登录服务器，执行运维命令','Ed25519 私钥认证，使用 Windows 原生 OpenSSH',215],
    ['DB','连接数据库，查询业务数据','PostgreSQL 与 MySQL，本机连接器内部取密',372],
    ['WEB','登录网站，继续操作页面','账号密码、仅密码表单、浏览器密码弹窗',529]
  ]){text(s,n,72,y-8,220,90,58,C.mint,true);text(s,label,330,y,870,55,35,C.white,true);text(s,sub,334,y+67,870,47,24,C.sub);}

  s=slide(C.mint,source+' 内部原始认证消息不进入 Agent 公共工具结果。业务结果仍可能包含敏感业务数据；本产品对受支持的直接凭据回显路径进行处理，不保证所有第三方路径。');
  text(s,'Agent 提出动作\nVault 在本机完成认证',72,78,1140,200,61,C.ink,true);
  text(s,'对话里的信息',76,364,520,52,29,C.ink,true);
  text(s,'连接名称、SQL、页面操作',76,442,530,112,34,C.ink);
  text(s,'本机使用的信息',722,364,490,52,29,C.ink,true);
  text(s,'密码、SSH 私钥、网站会话',722,442,490,112,34,C.ink);
  text(s,'受支持的认证流程不向模型返回原始密码与私钥',76,637,1120,42,24,C.ink);

  s=slide(C.dark,source+' SSH 实现：internal/vault/ssh.go。截图来自随机测试数据，非生产服务器。签名覆盖 SSH 用户认证数据，不是对任意字符串签名。当前没有逐次确认弹窗或待批准队列。');
  title(s,'SSH 私钥登录');
  text(s,'私钥由 Vault 保管',72,218,500,72,36,C.mint,true);
  text(s,'客户端请求认证签名\nVault 检查授权并签名\n服务器用公钥验证',76,333,490,185,29,C.white);
  image(s,'ssh',611,177,599,449);
  text(s,'保留主机密钥校验，由本机开关控制新的 SSH 签名',76,646,1130,42,23,C.sub);

  s=slide(C.white,source+' 数据库实现：internal/vault/database.go。每次调用结束关闭连接；结果行数、字节数及超时受限。账号权限决定读写，程序不靠 SQL 前缀强制只读。下列 SQL 仅为示例，无真实数据。');
  title(s,'数据库连接器自动取密',C.ink);
  text(s,'PostgreSQL  /  MySQL',72,211,1136,98,61,C.ink,true);
  text(s,'SELECT COUNT(*) FROM orders',76,372,1120,74,42,C.gray);
  text(s,'连接测试',76,536,310,54,31,C.ink,true);
  text(s,'表结构查询',457,536,330,54,31,C.ink,true);
  text(s,'带参数的 SQL',842,536,360,54,31,C.ink,true);
  text(s,'默认校验 TLS，实际读写权限由数据库账号决定',76,643,1120,42,23,C.gray);

  s=slide(C.dark,source+' 浏览器实现：internal/vault/browser.go；browser_cdp.go；password-form.js。账号密码使用私有 provider；仅密码路径通过本机私有浏览器连接注入。截图为测试配置，不含生产凭据。JavaScript prompt 不等同于 HTTP Basic 或系统弹窗。');
  text(s,'网页登录\n也可以只有密码',72,76,660,160,54,C.white,true);
  const modes=[['账号和密码','常规登录表单',295],['仅密码表单','只填写密码，无需账号',409],['浏览器密码弹窗','支持自动弹窗或按钮触发',523]];
  for(const [a,b,y] of modes){text(s,a,76,y,610,46,32,C.mint,true);text(s,b,76,y+53,610,42,25,C.sub);}
  image(s,'web',793,43,365,635,{crop:{left:0.26,top:0.04,right:0.26,bottom:0.04},fit:'contain'});

  s=slide(C.white,source+' 使用截图：.test-data/screenshots/product-dark.png，由 UI 自动化在独立测试保险库中生成。加密采用 DPAPI 包装密钥 + AES-256-GCM。使用记录最多保留最近 200 条，不是不可篡改合规审计。');
  text(s,'所有连接\n集中在本机',72,74,440,150,53,C.ink,true);
  text(s,'本机加密保存',76,330,420,56,29,C.ink,true);
  text(s,'按连接启用或停用',76,421,420,56,29,C.ink,true);
  text(s,'查看最近使用记录',76,512,420,56,29,C.ink,true);
  image(s,'product',516,136,704,528);

  s=slide(C.dark,source+' Skill 模板：internal/vault/integration-skill.md；MCP 实现：cmd/agent-vault/mcp.go。Skill 与 MCP 可共存，不是两套保险库。生成接入配置不代表所有宿主版本均完成兼容验收。');
  title(s,'Skill 与 MCP 两种接入方式');
  text(s,'Skill',72,220,520,115,88,C.white,true);
  text(s,'中文操作指引',76,373,520,60,35,C.mint,true);
  text(s,'Agent 按说明调用本机程序\n处理连接选择与错误状态',76,468,510,112,29,C.sub);
  text(s,'MCP',726,220,490,115,88,C.mint,true);
  text(s,'可发现的结构化工具',730,373,490,60,35,C.white,true);
  text(s,'宿主直接调用固定工具\n共用同一套本机凭据能力',730,468,490,112,29,C.sub);

  s=slide(C.dark,source+' 源码：cmd/agent-vault/mcp.go；internal/vault/ipc_windows.go；internal/vault/host.go。固定公开工具共 11 个，未提供密码读取、ssh_exec 或任意 Shell。采用 JSON-RPC stdio，转发 Windows 命名管道，不提供远程 HTTP MCP endpoint。外部配置依据：https://learn.chatgpt.com/docs/extend/mcp?surface=cli；https://code.claude.com/docs/en/mcp。');
  title(s,'MCP 的请求如何到达 Vault');
  const steps=[['01','宿主启动本地 MCP 进程','agent-vault.exe mcp',215],['02','通过标准输入与输出通信','JSON-RPC 工具调用',325],['03','请求转发到本机 Vault','Windows 命名管道',435],['04','认证组件执行并返回结果','密码不作为公共工具参数',545]];
  for(const [n,a,b,y] of steps){text(s,n,76,y,75,56,34,C.mint,true);text(s,a,178,y-1,600,52,30,C.white,true);text(s,b,180,y+52,600,42,24,C.sub);}
  text(s,'11',869,235,320,185,151,C.mint,true);
  text(s,'个公开工具',872,438,320,57,37,C.white,true);
  text(s,'SSH 提供连接信息\n远程命令仍需原生客户端',874,548,330,91,23,C.sub);

  s=slide(C.mint,source+' 实际准备顺序：启动并解锁后台 Vault，本机添加连接并启用，安装 Skill 或自行注册 MCP，在新的宿主会话中按名称发起任务。SSH 还需服务器公钥配置与本机接入开关。宿主加载兼容性以实际版本验收为准。');
  text(s,'一次配置\n在新会话中使用',72,74,1136,165,57,C.ink,true);
  const start=[['01','启动保险库','在本机创建或解锁'],['02','添加连接','只在本机录入凭据'],['03','接入 Agent','安装 Skill 或配置 MCP'],['04','提出任务','使用保存的连接名称']];
  for(let i=0;i<start.length;i++){const [n,a,b]=start[i],x=76+i*293;text(s,n,x,354,235,92,67,C.ink,true);text(s,a,x,466,255,58,31,C.ink,true);text(s,b,x,547,260,85,24,C.ink);}

  s=slide(C.white,source+' 保护边界依据：README.md；docs/windows-agent-vault-constraints.md。没有逐次审批或命令白名单；同用户恶意进程和管理员不在强隔离边界内；截图、转换内容和任意第三方输出不作绝对保证。');
  title(s,'当前保护范围',C.ink);
  text(s,'已实现',76,224,520,62,35,C.ink,true);
  text(s,'支持路径不返回原始凭据\n本机加密保存与按连接停用\n最近使用记录便于本机追踪',76,338,548,216,28,C.gray);
  text(s,'明确边界',727,224,480,62,35,C.ink,true);
  text(s,'不隔离同账号恶意程序\n不覆盖所有第三方返回路径\n不提供命令白名单或逐次审批',727,338,485,216,28,C.gray);
  text(s,'授权依据：本机解锁、连接启用，以及对应接入状态',76,639,1136,42,24,C.gray);

  s=slide(C.dark,source+' 封面概念插画复用为结束页背景。建议以随机测试凭据验收，不使用生产凭据演示，也不在聊天中录入密码。');
  image(s,'cover',0,0,1280,720,{fit:'cover'});
  text(s,'Agent Vault',72,84,700,75,49,C.white,true);
  text(s,'下一次登录\n从连接名称开始',72,256,680,185,64,C.white,true);
  text(s,'密码与私钥，在本机窗口录入',76,545,640,49,27,C.mint);
  return {slides:p.slides.items.length,font:'Microsoft YaHei',canvas:'1280x720'};
}
