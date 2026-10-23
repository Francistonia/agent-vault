(function(config) {
  try {
    if (location.origin !== config.origin) return {ok:false,code:'ORIGIN_MISMATCH'};
    const visible = el => {
      const style=getComputedStyle(el),rect=el.getBoundingClientRect();
      return el.isConnected&&!el.disabled&&!el.readOnly&&style.visibility!=='hidden'&&style.display!=='none'&&Number(style.opacity)!==0&&rect.width>0&&rect.height>0;
    };
    const candidates=Array.from(document.querySelectorAll(config.selector||'input[type=password]')).filter(visible);
    if (!candidates.length) return {ok:false,code:'WAITING_FOR_FIELD'};
    if (candidates.length!==1) return {ok:false,code:'AMBIGUOUS_FIELD'};
    const el=candidates[0];
    if (!(el instanceof HTMLInputElement)||['hidden','file','button','submit','checkbox','radio'].includes(el.type)) return {ok:false,code:'INVALID_FIELD'};
    // Use an isolated JavaScript world and the native input setter. The value
    // is supplied privately over local CDP, never through the model tool API.
    const setter=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set;
    el.focus();setter.call(el,config.password);
    el.dispatchEvent(new Event('input',{bubbles:true}));
    el.dispatchEvent(new Event('change',{bubbles:true}));
    if (location.origin!==config.origin||!el.isConnected) return {ok:false,code:'PAGE_CHANGED'};
    return {ok:true};
  } catch (_) { return {ok:false,code:'FORM_CONFIGURATION_ERROR'}; }
})
