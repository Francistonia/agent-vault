'use strict';
const $=s=>document.querySelector(s);
const main=$('#main'),dialogRoot=$('#dialog-root');
let state=null,page='targets',filter='all',search='',selected=null,busy=false,lastFocus=null,toastTimer,confirmResolve=null;
const kinds={ssh:{name:'SSH 服务器',icon:'server'},db:{name:'数据库',icon:'database'},web:{name:'网站',icon:'browser'}};
const esc=v=>String(v??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const icon=(name,extra='')=>`<svg class="icon ${extra}" aria-hidden="true"><use href="#av-icon-${name}"></use></svg>`;
const btn=(label,attrs='',primary=false,glyph='')=>`<button type="button" class="button${primary?' primary':''}" ${attrs}>${glyph?icon(glyph):''}<span>${label}</span></button>`;
const badge=(label,type='',glyph='')=>`<span class="badge ${type}">${glyph?icon(glyph):'<span class="status-dot"></span>'}${esc(label)}</span>`;
const kv=(key,value)=>`<div class="kv"><span>${esc(key)}</span><span>${esc(value)}</span></div>`;
function heading(title,sub,actions=''){return `<div class="heading"><div><h1>${title}</h1><p class="subtitle">${sub}</p></div>${actions}</div>`}
async function api(path,data){
  const options={headers:{'X-Agent-Vault':'local-ui'}};
  if(data!==undefined){options.method='POST';options.headers['Content-Type']='application/json';options.body=JSON.stringify(data)}
  const r=await fetch('/api/'+path,options);
  if(!r.ok)throw new Error('管理连接已断开，请重新打开 Agent Vault。');
  const value=await r.json();
  if(!value.ok){const e=new Error(value.error?.message||'操作未完成，请稍后重试。');e.code=value.error?.code;throw e}
  return value.data;
}
function toast(message){$('#toast').innerHTML=icon('check')+`<span>${esc(message)}</span>`;$('#toast').hidden=false;clearTimeout(toastTimer);toastTimer=setTimeout(()=>$('#toast').hidden=true,4200)}
async function refresh(){try{state=await api('state');render()}catch(e){main.innerHTML=`<div class="error-panel">${esc(e.message)}</div>`}}
function render(){
  if(!state)return;
  document.querySelectorAll('[data-page]').forEach(b=>{b.classList.toggle('active',b.dataset.page===page);b.setAttribute('aria-current',b.dataset.page===page?'page':'false')});
  $('#lock-status').textContent=state.status.locked?'保险库已锁定':'保险库已解锁';
  $('#lock-button').innerHTML=icon(state.status.locked?'unlock':'lock')+`<span>${state.status.locked?'解锁保险库':'锁定保险库'}</span>`;
  if(state.status.locked){
    main.innerHTML=`<section class="locked"><div class="bigmark">${icon('vault')}</div><h1>${state.status.initialized?'保险库已锁定':'欢迎使用 Agent Vault'}</h1><p>${state.status.initialized?'解锁后，Agent 可按授权使用你的连接。<br>凭据始终由本机组件完成认证。':'为服务器、数据库和网站创建安全的登录入口。<br>从本机保险库开始管理你的连接。'}</p>${btn(state.status.initialized?'解锁保险库':'创建保险库','data-action="unlock"',true,state.status.initialized?'unlock':'plus')}<span class="lock-hint">${icon('shield')}由当前 Windows 账号保护</span></section>`;
    return;
  }
  main.innerHTML=`<div class="page">${page==='targets'?(selected?detail():targetsPage()):page==='agents'?agentsPage():page==='history'?historyPage():settingsPage()}</div>`;
}
function connectionBadge(t){
  if(!t.enabled)return badge('已停用');
  if(t.kind==='ssh'&&!state.status.ssh_client_enabled)return badge('接入已关闭','warn');
  return badge('已启用','success');
}
function targetsPage(){
  const all=state.targets||[];
  const targets=all.filter(t=>(filter==='all'||t.kind===filter)&&`${t.name} ${t.host} ${t.username}`.toLowerCase().includes(search.toLowerCase()));
  return heading('连接管理','集中管理服务器、数据库和网站的登录凭据。',btn('添加连接','data-action="add"',true,'plus'))+
    `<div class="callout"><span class="callout-icon">${icon('vault')}</span><div><strong>本地保存，按需认证</strong><p>在此设备录入凭据，让 Agent 按你的授权完成登录。</p></div>${badge('加密存储','success','shield')}</div>
    <div class="toolbar"><div class="filters" aria-label="连接类型">${[['all','全部'],['ssh','SSH'],['db','数据库'],['web','网站']].map(([k,v])=>`<button class="filter ${filter===k?'active':''}" data-filter="${k}" aria-pressed="${filter===k}">${v}<span class="filter-count">${k==='all'?all.length:all.filter(t=>t.kind===k).length}</span></button>`).join('')}</div><label class="searchbox">${icon('search')}<input class="search" id="search" aria-label="搜索连接" placeholder="搜索名称、地址或账号" value="${esc(search)}"></label></div>
    <div class="list"><div class="listhead"><span>连接名称</span><span>访问状态</span></div>${targets.length?targets.map(t=>`<button class="target" data-target="${esc(t.id)}"><span class="type-icon">${icon(kinds[t.kind].icon)}</span><span><strong>${esc(t.name)}</strong><span class="meta">${kinds[t.kind].name} · ${esc(t.kind==='web'?t.login_url:t.host)}</span></span>${connectionBadge(t)}${icon('chevron','row-chevron')}</button>`).join(''):`<div class="empty"><span class="empty-icon">${icon(all.length?'search':'vault')}</span><h2>${all.length?'没有找到匹配的连接':'添加第一个连接'}</h2><p>${all.length?'试试其他名称或地址，也可以切换连接类型。':'保存一组登录凭据，为 Agent 开启安全的连接方式。'}</p>${all.length?'':btn('添加连接','data-action="add"',true,'plus')}</div>`}</div>
    <div class="footnote"><span>${all.length} 个连接 · 仅保存在此设备</span><span>凭据由 Windows 系统保护</span></div>`;
}
function sshAccessButton(){return btn(state.status.ssh_client_enabled?'禁用 SSH 客户端接入':'启用 SSH 客户端接入','data-action="toggle-ssh"',!state.status.ssh_client_enabled,state.status.ssh_client_enabled?'lock':'plug')}
function mcpConfig(format){
  const c=state.mcp_config;
  if(format==='codex')return '[mcp_servers.agent-vault]\ncommand = '+JSON.stringify(c.command.replaceAll('\\','/'))+'\nargs = '+JSON.stringify(c.args.map(x=>x.replaceAll('\\','/')))+'\ntool_timeout_sec = 90';
  return JSON.stringify({mcpServers:{'agent-vault':{type:'stdio',command:c.command,args:c.args}}},null,2);
}
function detail(){
  const t=state.targets.find(x=>x.id===selected);
  if(!t){selected=null;return targetsPage()}
  const webMode=t.login_mode||'account_password',webModeName={account_password:'账号和密码',password_only:'仅密码表单',password_prompt:'浏览器密码弹窗'}[webMode];
  const connectionInfo=[['地址',t.kind==='web'?t.login_url:t.host],...(t.kind==='web'?[]:[['端口',t.port]]),...(t.username?[['登录账号',t.username]]:[]),['认证方式',t.kind==='ssh'?'Ed25519 私钥':t.kind==='db'?(t.engine==='mysql'?'MySQL':'PostgreSQL')+' 账号密码':webModeName],...(t.kind==='web'?[['连接保护',t.login_url.startsWith('https:')?'HTTPS':'HTTP · 未加密']]:[]),['连接授权',t.enabled?'允许使用':'已停用']];
  const ssh=t.kind==='ssh'?`<div class="code-toolbar"><h3>服务器公钥配置</h3>${btn('复制公钥',`data-copy-key="${esc(t.id)}"`,false,'copy')}</div><div class="code">${esc(t.public_key)}</div><p class="note">将此公钥加入服务器账号的 authorized_keys。私钥保存在保险库中，用于完成登录签名。</p>${kv('公钥指纹',t.fingerprint)}`:'';
  const database=t.kind==='db'?kv('数据库',t.database)+kv('连接保护',t.tls_mode==='disable'?'本机测试连接':'TLS · 校验服务器证书'):'';
  const side=t.kind==='ssh'?`<section class="panel"><h3>SSH 客户端接入</h3>${badge(state.status.ssh_client_enabled?'已开启':'已关闭',state.status.ssh_client_enabled?'success':'')}<p class="note">开启后，SSH 客户端可使用已授权的连接进行身份认证。</p>${sshAccessButton()}<p class="note">此开关影响全部 SSH 连接。已登录的远程会话需在客户端退出。</p></section>`:`<section class="panel"><h3>${t.kind==='db'?'连接测试':'网页登录'}</h3><p class="note">${t.kind==='db'?'验证当前账号能否连接到数据库。':'打开独立浏览器会话，使用本机保存的凭据登录。'}</p>${btn(t.kind==='db'?'测试连接':'打开并登录',`data-run="${esc(t.id)}"`,true,t.kind==='db'?'database':'browser')}<div id="operation-result" aria-live="polite"></div></section>`;
  return `<div class="back">${btn('返回连接', 'data-action="back"',false,'arrow-left')}</div>`+heading(esc(t.name),`${kinds[t.kind].name} · ${esc(t.username||webModeName)}`,btn('编辑连接',`data-edit="${esc(t.id)}"`))+`<div class="detail-layout"><section class="panel"><div class="panel-heading"><h3>连接信息</h3>${badge('已加密','','lock')}</div>${connectionInfo.map(([k,v])=>kv(k,v)).join('')}${database}${ssh}</section><div class="detail-side">${side}</div></div><div class="danger-zone"><button type="button" class="button danger" data-delete="${esc(t.id)}">${icon('trash')}<span>删除连接</span></button></div>`;
}
function agentsPage(){
  const agents={codex:{name:'Codex',icon:'terminal',description:'将本机连接带入你的开发工作流'},claude:{name:'Claude Code',icon:'spark',description:'在对话中调用已授权的登录能力'},openclaw:{name:'OpenClaw',icon:'bot',description:'为自动化任务提供本机认证入口'}};
  return heading('Agent 集成','连接你常用的 Agent，用自然语言使用保险库。')+`<div class="callout"><span class="callout-icon">${icon('plug')}</span><div><strong>一次安装，在新会话中使用</strong><p>集成会为当前 Windows 账号安装 Agent Vault 的使用说明。</p></div></div>${state.integrations.map(a=>{const meta=agents[a.agent];return `<section class="app-row"><span class="icon-container">${icon(meta.icon)}</span><div><h3>${meta.name}</h3><p class="meta">${meta.description}</p><p class="meta app-description">${a.installed?icon('check'):''}${a.installed?'已安装 · 在新会话中使用':'尚未安装'}</p></div>${btn(a.installed?'更新':'安装集成',`data-install="${a.agent}"`,!a.installed,a.installed?'refresh':'plus')}</section>`}).join('')}<section class="panel technical"><h3>MCP 接入</h3><p class="note">通过本地工具协议接入兼容客户端。保持 Agent Vault 运行，在客户端添加以下配置后重新加载。</p><details><summary>Codex 配置</summary><p class="note">合并到用户配置文件 ~/.codex/config.toml，保留已有设置。</p><div class="code">${esc(mcpConfig('codex'))}</div>${btn('复制 Codex 配置','data-copy-mcp="codex"',false,'copy')}</details><details><summary>通用 JSON 配置</summary><p class="note">适用于支持 mcpServers 的客户端；项目配置需在宿主中确认加载。</p><div class="code">${esc(mcpConfig('json'))}</div>${btn('复制 JSON 配置','data-copy-mcp="json"',false,'copy')}</details><p class="note">无需在配置中填写密码。SSH 工具提供连接信息，远程命令仍使用原生 SSH 客户端。</p></section>`;
}
const actionNames={'ssh.sign':'SSH 认证签名','db.test':'数据库连接测试','db.query':'数据库查询','db.schema':'读取数据库结构','browser.login':'网站登录'};
const statusNames={allowed:'已授权',started:'已发起',connected:'已连接',success:'已完成',authenticated:'已确认登录',submitted:'已提交',filled:'已填写',failed:'未完成',denied:'已拒绝'};
function historyPage(){return heading('使用记录','查看保险库中的凭据何时、用于什么连接。')+`<section class="panel">${state.history.length?state.history.map(r=>`<div class="record"><span class="type-icon">${icon(kinds[r.kind]?.icon||'vault')}</span><div><h3>${esc(r.target_name)}</h3><div class="record-description"><span>${esc(actionNames[r.action]||'连接操作')}</span>${badge(statusNames[r.status]||'已记录',r.status==='failed'||r.status==='denied'?'warn':'')}${r.error_code?`<span>${esc(r.error_code)}</span>`:''}</div></div><time datetime="${esc(r.time)}">${esc(new Date(r.time).toLocaleString('zh-CN',{month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit'}))}</time></div>`).join(''):`<div class="empty"><span class="empty-icon">${icon('clock')}</span><h2>还没有使用记录</h2><p>通过 Agent 使用连接后，认证和连接记录会显示在这里。</p></div>`}</section><div class="footnote"><span>最近 ${state.history.length} 条记录 · 最多保留 200 条</span><span>记录随保险库一起加密保存</span></div>`}
function sshDiagnostics(s){
  const a=s.ssh_last_attempt;
  return `<details class="technical"><summary>查看认证诊断</summary><p class="note">接入开启、保险库解锁且连接已授权时自动签名。</p>${a?kv('最近签名',a.status==='signed'?'签名已完成':'签名未完成')+kv('时间',new Date(a.time).toLocaleString())+(a.method?kv('认证方法',a.method):'')+(a.error_code?`<div class="result error">${esc(a.message)}<br>${esc(a.error_code)}</div>`:'<p class="note">签名已由本机完成，最终登录结果以服务器响应为准。</p>'):'<p class="note">暂未收到签名请求。</p>'}<p class="note">签名按连接授权自动执行，没有逐次确认流程。失败请求不会进入待批准队列。</p></details>`;
}
function settingsPage(){
  const s=state.status;
  return heading('设置','管理此设备上的保险库与登录接入。',btn('刷新状态','data-action="refresh"',false,'refresh'))+`<div class="settings-stack">
  <section class="panel"><div class="panel-heading"><div><h3>SSH 客户端接入</h3><p class="meta">管理所有 SSH 连接的签名入口</p></div>${badge(s.ssh_client_enabled?'已开启':'已关闭',s.ssh_client_enabled?'success':'')}</div><div class="setting-row"><div><h3>允许 SSH 身份认证</h3><p class="meta">为已授权的 SSH 连接提供本机私钥签名。</p></div>${sshAccessButton()}</div><p class="note">禁用后停止新的签名。已建立的远程会话需在 SSH 客户端中退出。</p>${sshDiagnostics(s)}</section>
  <section class="panel"><h3>设备与组件</h3>${kv('应用版本',s.version)}${kv('数据库支持','PostgreSQL · MySQL')}${kv('浏览器组件',s.browser_installed?'已就绪':'未找到浏览器组件')}<details class="technical"><summary>查看技术信息</summary>${kv('本机加密',s.storage)}${kv('标准 SSH 入口',s.ssh_standard_enabled?'已由 Agent Vault 启用':'未启用')}${kv('专用管道',s.ssh_pipe)}<p class="note">浏览器登录需要已安装 Chrome。组件缺失时，请按使用说明重新安装完整产品包。</p></details></section>
  <section class="panel"><div class="panel-heading"><h3>安全与隐私</h3>${icon('shield')}</div><p class="note privacy-text">密码与私钥在本机加密保存。Agent 通过连接名称调用本地认证能力，支持的登录路径不向模型提供原始凭据。</p><details><summary>了解保护范围</summary><div class="note privacy-text"><p>保险库使用当前 Windows 账号的系统保护，未提供独立主密码或 Windows Hello 验证。同账号进程及管理员访问不属于强隔离边界。</p><p>锁定、停用和关闭接入会阻止后续凭据使用，已交付凭据和已登录会话无法因此追溯撤回。</p><p>第三方工具的未知输出、截图和转换内容需要单独验证。使用记录用于本机追踪，不提供防篡改合规审计保证。</p></div></details></section>
  <section class="panel"><div class="setting-row"><div><h3>退出 Agent Vault</h3><p class="meta">停止此设备上的保险库服务。</p></div>${btn('退出应用','data-action="quit"',false,'power')}</div></section></div>`;
}
function closeDialog(answer=false){
  const resolve=confirmResolve;confirmResolve=null;
  dialogRoot.replaceChildren();main.inert=false;$('aside').inert=false;
  if(lastFocus?.isConnected)lastFocus.focus();
  if(resolve)resolve(answer);
}
function confirmAction(title,message,label,glyph='trash'){
  lastFocus=document.activeElement;main.inert=true;$('aside').inert=true;
  dialogRoot.innerHTML=`<div class="overlay"><section class="dialog confirm-dialog" role="alertdialog" aria-modal="true" aria-labelledby="confirm-title" aria-describedby="confirm-message"><span class="empty-icon">${icon(glyph)}</span><h2 id="confirm-title">${esc(title)}</h2><p id="confirm-message">${esc(message)}</p><div class="dialog-footer">${btn('取消','data-action="close"')}<button type="button" class="button danger" data-action="confirm">${esc(label)}</button></div></section></div>`;
  $('[data-action="close"]').focus();
  return new Promise(resolve=>confirmResolve=resolve);
}
function showDialog(kind='ssh',id=null){
  lastFocus=document.activeElement;const t=state.targets.find(t=>t.id===id);main.inert=true;$('aside').inert=true;
  dialogRoot.innerHTML=`<div class="overlay"><section class="dialog" role="dialog" aria-modal="true" aria-labelledby="dialog-title"><div class="dialog-head"><div><h2 id="dialog-title">${t?'编辑连接':'添加连接'}</h2><p class="meta">在此设备保存登录信息，供 Agent 按授权使用。</p></div><button class="close" data-action="close" aria-label="关闭">${icon('x')}</button></div>${t?'':`<div class="filters" aria-label="连接类型">${Object.entries(kinds).map(([k,v])=>`<button class="filter ${k===kind?'active':''}" data-formkind="${k}" aria-pressed="${k===kind}">${icon(v.icon)}${v.name}</button>`).join('')}</div>`}<form id="target-form" data-kind="${kind}" data-id="${id||''}" autocomplete="off"><div id="fields">${fields(kind,t)}</div><div id="form-error" class="error" role="alert"></div><div class="dialog-footer">${btn('取消','data-action="close"')}<button type="submit" class="button primary">保存连接</button></div></form></section></div>`;
  updateWebFields($('#target-form'));$('#target-form [name="name"]').focus();
}
function fields(kind,t={}){
  t=t||{};
  const f=(name,label,value='',full=false,type='text',required=true)=>`<label class="field ${full?'full':''}">${label}<input name="${name}" type="${type}" value="${esc(value)}" ${required?'required':''} autocomplete="off"></label>`;
  const webMode=t.login_mode||'account_password';
  return `<div class="formgrid">${f('name','连接名称',t.name||'',true)}${kind==='db'?`<label class="field full">数据库类型<select name="engine"><option value="postgres" ${t.engine==='postgres'?'selected':''}>PostgreSQL</option><option value="mysql" ${t.engine==='mysql'?'selected':''}>MySQL</option></select></label>`:''}${f(kind==='web'?'login_url':'host',kind==='web'?'登录网址':'服务器地址',kind==='web'?t.login_url:t.host,true)}${kind==='web'?`<label class="field full">登录方式<select name="login_mode">${[['account_password','账号和密码'],['password_only','仅密码表单'],['password_prompt','浏览器密码弹窗']].map(([v,l])=>`<option value="${v}" ${webMode===v?'selected':''}>${l}</option>`).join('')}</select><small id="login-mode-hint"></small></label>`:''}${f('username',kind==='web'?'登录账号':'登录用户名',t.username||'',kind==='web')}${kind!=='web'?f('port','端口',t.port||(kind==='ssh'?22:5432),false,'number'):''}${kind==='db'?f('database','数据库名称',t.database||'',true)+`<label class="field full">连接保护<select name="tls_mode"><option value="verify-full">TLS · 校验服务器证书</option><option value="disable" ${t.tls_mode==='disable'?'selected':''}>本机测试 · 不使用 TLS</option></select></label>`:''}${kind==='ssh'?`<label class="field full file-label">${t.id?'替换私钥文件':'私钥文件'}<input type="file" name="key_file" accept=".pem,.key,*"><small>支持 Ed25519，OpenSSH 或 PEM 格式。${t.id?'不选择文件则保留原私钥。':'文件仅在本机读取。'}</small></label>${f('passphrase','私钥口令（可选）','',true,'password',false)}`:f('password',t.id?'更新密码（留空保留原密码）':'登录密码','',true,'password',!t.id)}${kind==='web'?`<label class="check-row" id="http-option"><input name="allow_http" type="checkbox" ${t.allow_http?'checked':''}><span>允许此连接使用 HTTP<small>HTTP 不加密网络传输。仅在确认网站要求时启用；本机测试地址无需启用。</small></span></label><details class="form-advanced"><summary>高级登录选项</summary><p class="note">选择器可留空；需要指定页面元素时再填写。</p><label class="field">打开登录入口<input name="login_trigger_selector" value="${esc(t.login_trigger_selector)}" placeholder="例如 #unlock"><small>页面打开后需先点击按钮时填写；自动出现的弹窗留空。</small></label><label class="field">账号输入框<input name="username_selector" value="${esc(t.username_selector)}" placeholder="CSS 选择器，例如 #email"></label><label class="field">密码输入框<input name="password_selector" value="${esc(t.password_selector)}" placeholder="例如 #password"></label><label class="field">登录按钮<input name="submit_selector" value="${esc(t.submit_selector)}" placeholder="例如 button[type=submit]"></label><label class="field">登录成功标记<input name="success_selector" value="${esc(t.success_selector)}" placeholder="例如 #dashboard"><small>配置后根据此页面元素确认登录结果。</small></label></details>`:''}<label class="check-row"><input name="enabled" type="checkbox" ${!t.id||t.enabled?'checked':''}><span>允许 Agent 使用此连接<small>可随时停用授权，已登录的会话继续有效。</small></span></label></div>`;
}
function updateWebFields(form){
  if(form.dataset.kind!=='web')return;
  const mode=form.elements.login_mode.value;
  for(const name of ['username','username_selector','password_selector','submit_selector','login_trigger_selector']){
    const input=form.elements.namedItem(name),show=name.startsWith('username')?mode==='account_password':name==='login_trigger_selector'?mode!=='account_password':mode!=='password_prompt';
    input.closest('label').hidden=!show;input.disabled=!show;input.required=name==='username'&&show;
  }
  $('#login-mode-hint').textContent={account_password:'适用于需要填写账号和密码的登录表单。',password_only:'只填写页面中的密码输入框，无需账号。',password_prompt:'在浏览器原生输入弹窗中提交密码，无需账号。'}[mode];
}
async function saveTarget(form){
  if(busy)return;busy=true;const save=form.querySelector('button[type=submit]');save.disabled=true;
  try{
    const get=n=>{const input=form.elements.namedItem(n);return input&&!input.disabled?input.value:''};const kind=form.dataset.kind;
    const target={id:form.dataset.id,kind,name:get('name'),host:get('host'),username:get('username'),enabled:form.elements.enabled.checked,port:kind==='web'?0:Number(get('port')),database:get('database'),engine:get('engine'),tls_mode:get('tls_mode'),login_url:get('login_url'),login_mode:get('login_mode'),allow_http:!!form.elements.namedItem('allow_http')?.checked,login_trigger_selector:get('login_trigger_selector'),username_selector:get('username_selector'),password_selector:get('password_selector'),submit_selector:get('submit_selector'),success_selector:get('success_selector')};
    let privateKey='';const file=form.elements.namedItem('key_file')?.files[0];
    if(file){if(file.size>128*1024)throw new Error('私钥文件过大，请检查所选文件。');privateKey=await file.text()}
    const payload={target,password:get('password'),private_key:privateKey,passphrase:get('passphrase')};
    const result=await api('targets/save',payload);payload.password='';payload.private_key='';payload.passphrase='';
    form.reset();closeDialog();selected=result.id;await refresh();toast('连接已保存');
  }catch(e){if($('#form-error'))$('#form-error').textContent=e.message}
  finally{busy=false;save.disabled=false}
}
document.addEventListener('click',async e=>{
  const b=e.target.closest('button');if(!b)return;
  if(b.dataset.page){selected=null;page=b.dataset.page;render();return}
  if(b.dataset.filter){filter=b.dataset.filter;render();return}
  if(b.dataset.target){selected=b.dataset.target;render();return}
  if(b.dataset.edit){const t=state.targets.find(t=>t.id===b.dataset.edit);showDialog(t.kind,t.id);return}
  if(b.dataset.formkind){const form=$('#target-form');form.reset();form.dataset.kind=b.dataset.formkind;$('#fields').innerHTML=fields(b.dataset.formkind);updateWebFields(form);dialogRoot.querySelectorAll('[data-formkind]').forEach(x=>{x.classList.toggle('active',x===b);x.setAttribute('aria-pressed',x===b)});return}
  try{
    if(b.dataset.copyMcp){await navigator.clipboard.writeText(mcpConfig(b.dataset.copyMcp));toast('MCP 配置已复制');return}
    if(b.dataset.copyKey){const t=state.targets.find(t=>t.id===b.dataset.copyKey);await navigator.clipboard.writeText(t.public_key);toast('公钥已复制');return}
    if(b.dataset.install){b.disabled=true;await api('integrations/install',{agent:b.dataset.install});await refresh();toast('集成已安装，请在 Agent 的新会话中使用');return}
    if(b.dataset.delete){
      if(!await confirmAction('删除此连接？','此设备保存的连接与凭据将被删除。服务器授权和已登录会话需要在对应系统中管理。','删除连接'))return;
      await api('targets/delete',{id:b.dataset.delete});selected=null;await refresh();toast('连接已删除');return;
    }
    if(b.dataset.run){
      b.disabled=true;const t=state.targets.find(t=>t.id===b.dataset.run);
      const result=await api('operation',{version:1,op:t.kind==='db'?'db.test':'browser.login',target:t.id});
      $('#operation-result').innerHTML=`<div class="result">${esc(t.kind==='db'?(result.connected?'连接成功，数据库已响应。':'连接测试未完成。'):(result.status==='authenticated'?'登录成功，已检测到配置的页面标记。':result.status==='filled'?'密码已填写，尚未提交。请在浏览器中继续。':'登录信息已提交，请在浏览器中确认结果。'))}</div>`;
      state=await api('state');return;
    }
    if(b.id==='lock-button'){await api(state.status.locked?'unlock':'lock',{});selected=null;await refresh();return}
    switch(b.dataset.action){
      case 'unlock':await api('unlock',{});await refresh();break;
      case 'lock':await api('lock',{});await refresh();break;
      case 'add':showDialog();break;
      case 'back':selected=null;render();break;
      case 'close':closeDialog();break;
      case 'confirm':closeDialog(true);break;
      case 'refresh':await refresh();toast('状态已更新');break;
      case 'toggle-ssh':{b.disabled=true;const enable=!state.status.ssh_client_enabled;await api(enable?'ssh/enable':'ssh/disable',{});await refresh();toast(enable?'SSH 客户端接入已开启':'SSH 客户端接入已关闭');break;}
      case 'quit':if(await confirmAction('退出 Agent Vault？','保险库服务将停止，新的认证请求无法继续。已建立的远程会话不受影响。','退出应用','power')){await api('shutdown',{});main.innerHTML=`<section class="locked"><div class="bigmark">${icon('vault')}</div><h1>保险库已关闭</h1><p>重新打开 Agent Vault 即可继续使用。</p></section>`;$('#lock-status').textContent='服务已停止'}break;
    }
  }catch(err){if(b.dataset.run&&$('#operation-result'))$('#operation-result').innerHTML=`<div class="result error">${esc(err.message)}</div>`;else toast(err.message)}
  finally{b.disabled=false}
});
document.addEventListener('submit',e=>{if(e.target.id==='target-form'){e.preventDefault();saveTarget(e.target)}});
document.addEventListener('input',e=>{if(e.target.id==='search'){search=e.target.value;const start=e.target.selectionStart,scroll=main.scrollTop;render();$('#search').focus({preventScroll:true});$('#search').setSelectionRange(start,start);main.scrollTop=scroll}});
document.addEventListener('change',e=>{if(e.target.name==='login_mode')updateWebFields(e.target.form);if(e.target.name==='engine'){const form=e.target.form,port=form.elements.port;if(!form.dataset.id&&['5432','3306'].includes(port.value))port.value=e.target.value==='mysql'?'3306':'5432'}});
document.addEventListener('keydown',e=>{
  const dialog=$('.dialog');if(!dialog)return;
  if(e.key==='Escape'){e.preventDefault();closeDialog();return}
  if(e.key==='Tab'){
    const items=[...dialog.querySelectorAll('button,input,select,textarea,summary,a[href]')].filter(x=>!x.disabled&&x.getClientRects().length);
    if(!items.length)return;
    if(e.shiftKey&&document.activeElement===items[0]){e.preventDefault();items.at(-1).focus()}
    else if(!e.shiftKey&&document.activeElement===items.at(-1)){e.preventDefault();items[0].focus()}
  }
});
refresh();
