(function(){
'use strict';

var CHUNK_SIZE=64*1024;
var ws,me,users={},room='',pwd='';
var offers={},xfers={},tcnt=0;
var apiBase='';

var GRAY='#8E8E93';

function q(n){return new URLSearchParams(window.location.search).get(n)||'';}

function roomFromPath(){
  var s=location.pathname.replace(/^\//,'').split('/')[0];
  return s||'index';
}
function init(){
  pwd=q('pwd'); if(!pwd){window.location.href='/';return;}
  document.getElementById('myName').textContent='…';
  fetch('/api/config').then(function(r){return r.json()}).then(function(cfg){
    apiBase=cfg.public_url;
    fetch(apiBase+'/api/username').then(function(r){return r.json()}).then(function(d){
      me={id:d.name+'_'+Date.now(),name:d.name,color:'#27C5F5'};
      document.getElementById('myName').textContent='你: '+d.name;
      var rn=roomFromPath();
      localStorage.setItem('lastRoom',rn);joinRoom(rn);
    });
    fetch(apiBase+'/api/version').then(function(r){return r.json()}).then(function(d){document.getElementById('ver').textContent='v'+d.version;});
  });
  document.getElementById('upInput').addEventListener('change',function(){upFile(this);});
  setBtn('out');
}

/* ---- WS ---- */
function wsConn(){
  var base=apiBase||(location.protocol+'//'+location.host);
  var wsBase=base.replace(/^http/,'ws');
  ws=new WebSocket(wsBase+'/ws?pwd='+encodeURIComponent(pwd));
  ws.onopen=function(){t('已连接');if(room)sendJoin();};
  ws.onclose=function(){t('重连中…','err');setTimeout(wsConn,3000);};
  ws.onmessage=function(e){try{onMsg(JSON.parse(e.data))}catch(_){}};
  ws.onerror=function(){};
}
function send(d){if(ws&&ws.readyState===WebSocket.OPEN)ws.send(JSON.stringify(d));}
function onMsg(m){
  switch(m.type){
    case'room-users':users={};m.users.forEach(function(u){if(!u.is_me)u.color=GRAY;users[u.id]=u});draw();break;
    case'user-joined':m.user.color=GRAY;users[m.user.id]=m.user;draw();t(m.user.name+' 加入了');break;
    case'user-left':delete users[m.user_id];draw();t('用户离开');break;
    case'file-offer':onOffer(m);break;
    case'file-accept':onAccept(m);break;
    case'file-reject':onReject(m);break;
    case'file-chunk':onChunk(m);break;
    case'file-cancel':onCancel(m);break;
    case'chunk-ack':onChunkAck(m);break;
    case'text':onText(m);break;
  }
}

/* ---- Room ---- */
function showJoinModal(val){
  var h='<h3>加入房间</h3><input class="big-input" id="rmInput" placeholder="输入房间号" maxlength="50"'+(val?' value="'+val+'"':'')+' autofocus>';
  modal(h,[{l:'取消',c:'secondary',fn:closeM},{l:'加入',c:'primary',fn:function(){var v=document.getElementById('rmInput').value.trim();if(v){closeM();joinRoom(v);}else t('请输入房间号','err')}}]);
  var i=document.getElementById('rmInput');
  if(i){i.focus();i.onkeydown=function(e){if(e.key==='Enter'){var v=i.value.trim();if(v){closeM();joinRoom(v);}}}}
}
function joinRoom(v){
  room=v;users={};
  localStorage.setItem('lastRoom',v);
  document.getElementById('roomName').textContent='房间: '+v;
  setBtn('in');
  document.getElementById('userGrid').innerHTML='<div class="empty"><div class="big">◌</div><p>连接中…</p></div>';
  history.replaceState({},'','/'+encodeURIComponent(v)+'?pwd='+encodeURIComponent(pwd));
  if(!ws||ws.readyState!==WebSocket.OPEN)wsConn();else sendJoin();
}
function sendJoin(){send({type:'join',room:room,user:{id:me.id,name:me.name,color:me.color}});}
function leaveRoom(){
  if(ws)ws.close();
  room='';users={};
  document.getElementById('roomName').textContent='';
  setBtn('out');
  draw();
}
function setBtn(s){
  var fc=document.getElementById('footContent');
  if(s==='in'){
    fc.innerHTML='<div class="btns-row">'+
      '<button class="big-btn" id="upBtn">上传文件</button>'+
      '<button class="switch-btn" id="switchBtn">切换房间</button>'+
      '<button class="qr-btn" id="qrBtn" title="房间二维码" aria-label="房间二维码"><svg viewBox="0 0 24 24" fill="currentColor"><path d="M3 11h8V3H3v8zm2-6h4v4H5V5zm8-2v8h8V3h-8zm6 6h-4V5h4v4zM3 21h8v-8H3v8zm2-6h4v4H5v-4zm13-2h-2v3h-2v2h2v2h2v-2h2v-2h-2v-3zm-2 7h2v2h-2v-2zm4-4h2v2h-2v-2z"/></svg></button>'+
    '</div>';
    document.getElementById('upBtn').onclick=function(){document.getElementById('upInput').click();};
    document.getElementById('switchBtn').onclick=function(){showJoinModal();};
    document.getElementById('qrBtn').onclick=showRoomQR;
  }else{
    fc.innerHTML='<button class="big-btn" id="joinBtn">加入房间</button>';
    document.getElementById('joinBtn').onclick=function(){showJoinModal();};
  }
}

/* ---- Users ---- */
function draw(){
  var g=document.getElementById('userGrid'),ids=Object.keys(users);
  if(!ids.length){g.innerHTML='<div class="empty"><div class="big">◌</div><p>等待其他人…</p><div class="sub">分享房间号 "'+esc(room)+'"</div></div>';return;}
  g.innerHTML='';
  ids.sort(function(a,b){return a===me.id?-1:b===me.id?1:0});
  ids.forEach(function(id){
    var u=users[id],el=document.createElement('div');
    el.className='card'+(u.is_me?' me':' click');
    el.dataset.uid=id;
    el.innerHTML=
      '<div class="av" style="background:'+u.color+'"><div class="ring"></div>'+u.name.charAt(0).toUpperCase()+'</div>'+
      '<div class="nm">'+esc(u.name)+'</div>'+
      (u.is_me?'<div class="tag">我</div>':'<div class="off"></div>');
    if(!u.is_me){
      el.addEventListener('click',function(){sendFile(id);});
      el.addEventListener('contextmenu',function(e){e.preventDefault();txtDlg(id,u.name);});
      var t=null;
      el.addEventListener('touchstart',function(e){t=setTimeout(function(){t=null;e.preventDefault();txtDlg(id,u.name);},500);});
      el.addEventListener('touchend',function(){if(t){clearTimeout(t);t=null}});
      el.addEventListener('touchmove',function(){if(t){clearTimeout(t);t=null}});
    }
    g.appendChild(el);
  });
}

/* ---- File send ---- */
function sendFile(uid){
  var inp=document.createElement('input');inp.type='file';
  inp.onchange=function(e){
    var f=e.target.files[0];if(!f)return;
    var tf='tf_'+(++tcnt)+'_'+Date.now(),tgt=users[uid];
    send({type:'file-offer',to:uid,transfer_id:tf,file:{name:f.name,size:f.size,mime:f.type||'application/octet-stream'}});
    xfers[tf]={type:'send',file:f,userId:uid,target:tgt?tgt.name:'?',sent:0,total:f.size,ringUid:uid};
    t('已请求发送'+(tgt?' 给 '+tgt.name:''));
  };inp.click();
}
function onAccept(m){var x=xfers[m.transfer_id];if(!x||x.type!=='send')return;t(x.target+' 已接受');sendChunks(m.transfer_id,x);}
function onReject(m){var x=xfers[m.transfer_id];if(!x)return;t(x.target+' 已拒绝','err');delete xfers[m.transfer_id];}
function onChunkAck(m){
  var x=xfers[m.transfer_id];
  if(x&&x._ackWait&&x._ackWait.index===m.index)x._ackWait.fn();
}
function sendChunks(tid,x){
  var f=x.file,cs=CHUNK_SIZE,tc=Math.ceil(f.size/cs);
  x._retries={};
  function readChunk(index){
    return new Promise(function(resolve,reject){
      var s=index*cs,e=Math.min(s+cs,f.size);
      var r=new FileReader();
      r.onload=function(ev){resolve({data:ev.target.result.split(',')[1],size:e-s});};
      r.onerror=reject;
      r.readAsDataURL(f.slice(s,e));
    });
  }
  function sendOne(index){
    return new Promise(function(resolve,reject){
      var attempts=0;
      function attempt(){
        readChunk(index).then(function(chunk){
          var done=(index+1)>=tc;
          send({type:'file-chunk',to:x.userId,transfer_id:tid,data:chunk.data,index:index,total:tc,done:done});
          x.sent+=chunk.size;prog(tid,x.sent,x.total);
          var tmr=setTimeout(function(){
            attempts++;
            if(attempts>=5){reject(new Error('max retries'));return;}
            t('重传('+attempts+'/5)');attempt();
          },3000);
          x._ackWait={index:index,fn:function(){clearTimeout(tmr);resolve();}};
        });
      }
      attempt();
    });
  }
  (function next(i){if(i>=tc){x.done=true;t('已发送: '+f.name);setTimeout(function(){delete xfers[tid]},3000);progRm(tid);return;}
    sendOne(i).then(function(){next(i+1);}).catch(function(){t('发送失败','err');});
  })(0);
}

/* ---- File receive ---- */
function onOffer(m){
  offers[m.transfer_id]=m;
  modal('<h3>收到文件</h3><p><strong>'+esc(m.file.name)+'</strong> ('+sz(m.file.size)+')</p><p>来自 '+esc(m.from)+'</p>',[{l:'拒绝',c:'secondary',fn:function(){rej(m.transfer_id);closeM()}},{l:'接收',c:'primary',fn:function(){acc(m.transfer_id);closeM()}}]);
}
function acc(tid){var o=offers[tid];if(!o)return;send({type:'file-accept',to:o.from,transfer_id:tid});xfers[tid]={type:'receive',name:o.file.name,size:o.file.size,mime:o.file.mime,from:o.from,buf:[],rec:0,total:o.file.size,ringUid:o.from};delete offers[tid];t('接收中: '+o.file.name);}
function rej(tid){var o=offers[tid];if(!o)return;send({type:'file-reject',to:o.from,transfer_id:tid});delete offers[tid];t('已拒绝');}
function onChunk(m){
  var x=xfers[m.transfer_id];if(!x||x.type!=='receive')return;
  var bin=atob(m.data),b=new Uint8Array(bin.length);for(var i=0;i<bin.length;i++)b[i]=bin.charCodeAt(i);
  x.buf.push(b);x.rec+=b.length;prog(m.transfer_id,x.rec,x.total);
  send({type:'chunk-ack',to:m.from,transfer_id:m.transfer_id,index:m.index});
  if(m.done){x.done=true;fin(m.transfer_id,x);}
}
function fin(tid,x){
  var len=0,i;for(i=0;i<x.buf.length;i++)len+=x.buf[i].length;
  var all=new Uint8Array(len),off=0;for(i=0;i<x.buf.length;i++){all.set(x.buf[i],off);off+=x.buf[i].length;}
  var b=new Blob([all],{type:x.mime});
  (typeof showSaveFilePicker!=='undefined'?fsSave(b,x):dlBlob(b,x)).then(function(){t('已接收: '+x.name)}).catch(function(e){if(e.name!=='AbortError')dlBlob(b,x).then(function(){},function(){})});
  progRm(tid);setTimeout(function(){delete xfers[tid]},3000);
}
async function fsSave(b,x){var e=x.name.lastIndexOf('.')>-1?x.name.slice(x.name.lastIndexOf('.')):'';var h=await window.showSaveFilePicker({suggestedName:x.name,types:[{description:'文件',accept:{[x.mime]:[e||'.bin']}}]});var w=await h.createWritable();await w.write(b);await w.close();}
function dlBlob(b,x){return new Promise(function(r){var u=URL.createObjectURL(b),a=document.createElement('a');a.href=u;a.download=x.name;document.body.appendChild(a);a.click();setTimeout(function(){document.body.removeChild(a);URL.revokeObjectURL(u);r()},100)});}
function onCancel(m){var x=xfers[m.transfer_id];if(!x)return;t('传输已取消','err');delete xfers[m.transfer_id];progRm(m.transfer_id);}

/* ---- Text ---- */
function txtDlg(uid,name){
  modal('<h3>发送给 '+esc(name)+'</h3><textarea id="txIn" placeholder="输入消息…" maxlength="2000"></textarea>',[{l:'取消',c:'secondary',fn:closeM},{l:'发送',c:'primary',fn:function(){sendTxt(uid);closeM()}}]);
  var i=document.getElementById('txIn');if(i){i.focus();i.onkeydown=function(e){if(e.key==='Enter'&&!e.shiftKey){e.preventDefault();sendTxt(uid);closeM()}};}
}
function sendTxt(uid){var i=document.getElementById('txIn');if(!i)return;var c=i.value.trim();if(!c)return;send({type:'text',to:uid,content:c});t('已发送');}
function onText(m){
  var from=users[m.from],name=from?from.name:m.from;
  var ov=document.createElement('div');ov.className='overlay';
  ov.innerHTML='<div class="box"><h3>来自 '+esc(name)+'</h3><div style="max-height:280px;overflow-y:auto;margin-bottom:16px;padding:12px;background:var(--surface);border-radius:8px;font-size:13px;line-height:1.5;word-break:break-word;text-align:left">'+esc(m.content)+'</div><div class="btns" style="justify-content:space-between"><button class="secondary">关闭</button><button class="primary">复制</button></div></div>';
  ov.addEventListener('click',function(e){if(e.target===ov)ov.remove();});
  document.getElementById('modalRoot').appendChild(ov);
  var btns=ov.querySelectorAll('.btns button');
  btns[0].onclick=function(){ov.remove();};
  btns[1].onclick=function(){
    if(navigator.clipboard){navigator.clipboard.writeText(m.content).then(function(){t('已复制');ov.remove();});}
    else{var ta=document.createElement('textarea');ta.value=m.content;ta.style.position='fixed';ta.style.opacity='0';document.body.appendChild(ta);ta.select();document.execCommand('copy');document.body.removeChild(ta);t('已复制');ov.remove();}
  };
}

/* ---- Upload ---- */
function upFile(inp){
  var f=inp.files[0];if(!f)return;
  modal('<h3>上传选项</h3><p>文件: <strong>'+esc(f.name)+'</strong> ('+sz(f.size)+')</p><p style="color:var(--text2);font-size:13px;margin-bottom:8px">过期时间:</p><div style="display:flex;gap:6px;margin-bottom:14px">'+
    ['1天','3天','7天','14天','30天'].map(function(v){return '<button class="eopt" data-d="'+parseInt(v)+'" style="background:var(--surface);border:1px solid var(--border);border-radius:6px;padding:6px 14px;color:var(--text2);font-size:12px;cursor:pointer">'+v+'</button>'}).join('')+'</div>',
    [{l:'取消',c:'secondary',fn:function(){closeM();inp.value=''}},{l:'上传',c:'primary',id:'upGo',fn:function(){}}],
    function(){var bs=document.querySelectorAll('.eopt');[].forEach.call(bs,function(b){b.onclick=function(){bs.forEach(function(x){x.style.borderColor='var(--border)';x.style.color='var(--text2)'});this.style.borderColor='var(--accent)';this.style.color='var(--accent)';document.getElementById('upGo').dataset.exp=this.dataset.d}});bs[1].click();}
  );
  document.getElementById('upGo').dataset.exp='3';
  document.getElementById('upGo').onclick=function(){
    var expDays=parseInt(this.dataset.exp||'3');if(expDays<1)expDays=1;if(expDays>30)expDays=30;closeM();
    var file=f,CS=512*1024,total=Math.ceil(file.size/CS),fileId=Date.now()+'_'+Math.random().toString(36).substr(2,9),upId='up_'+fileId;
    t('上传中…');
    function upChunk(index){
      return new Promise(function(resolve,reject){
        var s=index*CS,e=Math.min(s+CS,file.size);
        var ffd=new FormData();
        ffd.append('file_id',fileId);ffd.append('index',index);ffd.append('total',total);
        ffd.append('name',file.name);ffd.append('mime',file.type||'application/octet-stream');
        ffd.append('expiry',expDays.toString());
        ffd.append('data',file.slice(s,e),'chunk_'+index);
        fetch(apiBase+'/api/upload/chunk?pwd='+encodeURIComponent(pwd),{method:'POST',body:ffd}).then(function(r){
          if(!r.ok)throw new Error('HTTP '+r.status);
          return r.json();
        }).then(function(res){resolve(res);}).catch(reject);
      });
    }
    function tryChunk(index,n){
      return upChunk(index).catch(function(err){
        if(n>=5)throw err;
        t('上传重试('+(n+1)+'/5)');
        return tryChunk(index,n+1);
      });
    }
    (function next(index){
      if(index>=total){progRm(upId);inp.value='';return;}
      tryChunk(index,0).then(function(res){
        prog(upId,index+1,total);
        if(res.done){showUploadResult(res);t('上传成功!');progRm(upId);inp.value='';}
        else next(index+1);
      }).catch(function(){t('上传失败','err');progRm(upId);inp.value='';});
    })(0);
  };
}
function showUploadResult(d){
  var u=(apiBase||window.location.protocol+'//'+window.location.host)+d.url;
  modal(
    '<h3>上传完成</h3>'+
    '<div style="text-align:left;margin-bottom:16px">'+
      '<div style="font-size:12px;color:var(--text2);margin-bottom:2px">文件</div>'+
      '<div style="font-size:14px;font-weight:500;margin-bottom:12px">'+esc(d.name)+'</div>'+
      '<div style="font-size:12px;color:var(--text2);margin-bottom:4px">下载链接</div>'+
      '<div style="display:flex;gap:6px">'+
        '<input id="dlUrl" type="text" value="'+esc(u)+'" readonly style="flex:1;background:var(--surface);border:1px solid var(--border);border-radius:8px;padding:10px 12px;font-size:13px;color:var(--text);outline:none">'+
        '<button id="cpBtn" style="background:var(--accent);color:#fff;border:none;border-radius:8px;padding:10px 14px;font-size:13px;font-weight:600;cursor:pointer;white-space:nowrap">复制</button>'+
      '</div>'+
      '<div style="text-align:center;margin-top:14px">'+qrImg(u)+'</div>'+
      '<div style="font-size:11px;color:var(--text2);text-align:center;margin-top:6px">扫码下载</div>'+
      '<div style="font-size:11px;color:var(--text2);margin-top:8px">过期: '+new Date(d.expires_at).toLocaleDateString()+'</div>'+
    '</div>',
    [{l:'关闭',c:'secondary',fn:closeM}],
    function(){
      var b=document.getElementById('cpBtn'),i=document.getElementById('dlUrl');
      if(b)b.onclick=function(){
        if(navigator.clipboard){navigator.clipboard.writeText(i.value).then(function(){b.textContent='已复制!';setTimeout(function(){b.textContent='复制'},2000);});}
        else{i.select();document.execCommand('copy');b.textContent='已复制!';setTimeout(function(){b.textContent='复制'},2000);}
      };
    }
  );
}

/* ---- QR ---- */
function shareBase(){return apiBase||(location.protocol+'//'+location.host);}
function qrImg(txt){
  try{
    var qr=qrcode(0,'M');
    qr.addData(txt);
    qr.make();
    var n=qr.getModuleCount(),cell=Math.max(2,Math.round(160/n));
    var src=qr.createDataURL(cell,2),px=(n+4)*cell;
    return '<img src="'+src+'" width="'+px+'" height="'+px+'" style="display:block;margin:0 auto;background:#fff;border:1px solid var(--border);border-radius:10px;padding:8px" alt="二维码">';
  }catch(e){return '';}
}
function roomQRUrl(){return shareBase()+'/'+encodeURIComponent(room)+'?pwd='+encodeURIComponent(pwd);}
function showRoomQR(){
  var u=roomQRUrl();
  modal(
    '<h3>房间二维码</h3>'+
    '<p style="font-size:13px;color:var(--text2);text-align:center;margin-bottom:14px">打开手机扫码加入房间</p>'+
    '<div style="text-align:center;margin-bottom:14px">'+qrImg(u)+'</div>'+
    '<div style="font-size:11px;color:var(--text2);word-break:break-all;text-align:center;line-height:1.5">'+esc(u)+'</div>',
    [{l:'关闭',c:'secondary',fn:closeM}]
  );
}

/* ---- Modal ---- */
function modal(body,buttons,after){
  var r=document.getElementById('modalRoot'),ov=document.createElement('div');ov.className='overlay';ov.id='m_'+Date.now();
  ov.innerHTML='<div class="box">'+body+'<div class="btns">'+buttons.map(function(b){return'<button'+(b.id?' id="'+b.id+'"':'')+' class="'+b.c+'">'+b.l+'</button>'}).join('')+'</div></div>';
  ov.addEventListener('click',function(e){if(e.target===ov)closeM(ov);});
  r.appendChild(ov);
  var bx=ov.querySelector('.box');
  buttons.forEach(function(b,i){var btn=bx.querySelectorAll('.btns button')[i];if(btn)btn.onclick=function(){b.fn()};});
  if(after)setTimeout(after,50);
}
function closeM(ov){if(!ov)ov=document.querySelector('.overlay:last-child');if(ov)ov.remove();}

/* ---- Toast ---- */
function t(msg,type){var c=document.getElementById('toasts'),el=document.createElement('div');el.className='toast'+(type?' '+type:'');el.textContent=msg;c.appendChild(el);setTimeout(function(){if(el.parentNode)el.remove()},3500);}

/* ---- Progress ---- */
function setRingProgress(uid,pct){
  var card=document.querySelector('.card[data-uid="'+uid+'"]');
  if(!card)return;
  var ring=card.querySelector('.ring');
  if(!ring)return;
  if(pct>0&&pct<100){
    var deg=(pct*3.6).toFixed(1)+'deg';
    ring.style.setProperty('--deg',deg);
    ring.style.background='conic-gradient(var(--accent) 0deg,var(--accent) '+deg+',transparent '+deg+',transparent 360deg)';
    ring.classList.add('on');
  }else{
    ring.classList.remove('on');
  }
}
function prog(id,rec,total){
  var el=document.getElementById('pr_'+id);
  if(!el){var c=document.getElementById('progs');el=document.createElement('div');el.className='prog';el.id='pr_'+id;el.innerHTML='<div class="pn"></div><div class="bar"><div class="fill" style="width:0%"></div></div><div class="pct"></div>';c.appendChild(el);}
  var p=total>0?Math.min(100,Math.round(rec/total*100)):0;el.querySelector('.pn').textContent=(rec/1024).toFixed(0)+'KB / '+(total/1024).toFixed(0)+'KB';el.querySelector('.pct').textContent=p+'%';el.querySelector('.fill').style.width=p+'%';
  var x=xfers[id];
  if(x&&x.ringUid)setRingProgress(x.ringUid,p);
}
function progRm(id){
  var el=document.getElementById('pr_'+id);if(el)el.remove();
  var x=xfers[id];
  if(x&&x.ringUid)setRingProgress(x.ringUid,0);
}

/* ---- Utils ---- */
function sz(b){if(b<1024)return b+'B';if(b<1048576)return(b/1024).toFixed(1)+'KB';if(b<1073741824)return(b/1048576).toFixed(1)+'MB';return(b/1073741824).toFixed(1)+'GB';}
function esc(s){var d=document.createElement('div');d.textContent=s;return d.innerHTML;}

document.addEventListener('DOMContentLoaded',init);
})();
