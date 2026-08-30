package jot

// wikiCSS is served from /assets/wiki.css so the CSP can stay free of
// unsafe-inline. The palette is defined once on :root and only re-bound for
// dark, so an explicit theme choice wins in both directions.
const wikiCSS = `
:root { color-scheme: light dark; --bg:#fbfaf7; --panel:#fff; --text:#24211d; --muted:#706b64; --line:#dedad2; --accent:#9b4d20; --code:#f1eee8; --mark:#ffe9a8; --markText:#3a2c00; --warn:#a4531b; }
@media (prefers-color-scheme:dark) { :root:not([data-theme="light"]) { --bg:#171614; --panel:#211f1c; --text:#ece8e1; --muted:#aaa39a; --line:#3b3833; --accent:#ee9b68; --code:#2b2925; --mark:#5a4a12; --markText:#ffeec2; --warn:#e9a468; } }
:root[data-theme="dark"] { --bg:#171614; --panel:#211f1c; --text:#ece8e1; --muted:#aaa39a; --line:#3b3833; --accent:#ee9b68; --code:#2b2925; --mark:#5a4a12; --markText:#ffeec2; --warn:#e9a468; }
* { box-sizing:border-box; }
body { margin:0; background:var(--bg); color:var(--text); font:17px/1.65 ui-serif,Georgia,Cambria,"Times New Roman",serif; }
a { color:var(--accent); text-decoration-thickness:.08em; text-underline-offset:.15em; }

header { border-bottom:1px solid var(--line); background:var(--panel); position:sticky; top:0; z-index:20; }
.bar { max-width:1400px; margin:auto; padding:.8rem 1.5rem; display:flex; align-items:center; gap:1rem; flex-wrap:wrap; }
.brand { color:var(--text); font:700 1.25rem/1 ui-sans-serif,system-ui,sans-serif; text-decoration:none; letter-spacing:-.02em; }
.nav { display:flex; gap:.9rem; font:600 .85rem ui-sans-serif,system-ui,sans-serif; }
.nav a { color:var(--muted); text-decoration:none; }
.nav a:hover, .nav a[aria-current="page"] { color:var(--accent); }
.search { position:relative; display:flex; flex:1; min-width:12rem; max-width:30rem; margin-left:auto; }
.search input { width:100%; padding:.55rem .75rem; border:1px solid var(--line); border-radius:.45rem 0 0 .45rem; background:var(--bg); color:var(--text); font:inherit; font-size:.95rem; }
.search button { border:1px solid var(--line); border-left:0; border-radius:0 .45rem .45rem 0; padding:.55rem .9rem; background:var(--accent); color:#fff; font:600 .85rem ui-sans-serif,system-ui,sans-serif; cursor:pointer; }
#theme { border:1px solid var(--line); background:var(--bg); color:var(--muted); border-radius:.45rem; padding:.45rem .7rem; font:600 .8rem ui-sans-serif,system-ui,sans-serif; cursor:pointer; }

/* live search dropdown */
.suggest { position:absolute; top:100%; left:0; right:0; background:var(--panel); border:1px solid var(--line); border-radius:0 0 .45rem .45rem; box-shadow:0 8px 24px rgba(0,0,0,.12); max-height:24rem; overflow-y:auto; z-index:30; }
.suggest a { display:block; padding:.5rem .75rem; text-decoration:none; color:var(--text); border-bottom:1px solid var(--line); }
.suggest a:last-child { border-bottom:0; }
.suggest a:hover, .suggest a.active { background:var(--code); }
.suggest strong { display:block; font:600 .9rem ui-sans-serif,system-ui,sans-serif; }
.suggest span { color:var(--muted); font-size:.8rem; }

.shell { max-width:1400px; margin:auto; display:grid; grid-template-columns:16rem minmax(0,1fr); gap:2rem; padding:0 1.5rem; align-items:start; }
.shell.solo { grid-template-columns:minmax(0,1fr); max-width:1000px; }
aside { position:sticky; top:4.2rem; padding:1.75rem 0; max-height:calc(100vh - 5rem); overflow-y:auto; font:ui-sans-serif,system-ui,sans-serif; }
aside h3 { font:700 .72rem ui-sans-serif,system-ui,sans-serif; text-transform:uppercase; letter-spacing:.08em; color:var(--muted); margin:0 0 .5rem; }
aside section { margin-bottom:1.75rem; }
aside ul { list-style:none; margin:0; padding:0; }
aside li { margin:.18rem 0; }
aside a { display:block; padding:.2rem .5rem; border-radius:.3rem; text-decoration:none; color:var(--text); font-size:.88rem; line-height:1.35; }
aside a:hover { background:var(--code); }
aside a[aria-current="page"] { background:var(--code); color:var(--accent); font-weight:600; }
aside .toc a { color:var(--muted); border-left:2px solid var(--line); border-radius:0; }
aside .toc a:hover { color:var(--accent); border-left-color:var(--accent); }
aside .toc .l3 { padding-left:1.1rem; } aside .toc .l4 { padding-left:1.8rem; }
aside details > summary { cursor:pointer; font:700 .72rem ui-sans-serif,system-ui,sans-serif; text-transform:uppercase; letter-spacing:.08em; color:var(--muted); margin-bottom:.5rem; }
aside details[open] > summary { margin-bottom:.5rem; }
aside .tree { font-size:.85rem; }
aside .tree > li > span { display:block; color:var(--muted); font-weight:600; padding:.25rem .5rem .1rem; font-size:.78rem; }

main { padding:2.25rem 0 5rem; min-width:0; }
h1,h2,h3,h4 { font-family:ui-sans-serif,system-ui,sans-serif; line-height:1.2; letter-spacing:-.025em; margin:2rem 0 .8rem; scroll-margin-top:4.5rem; }
h1 { font-size:2.3rem; margin-top:0; } h2 { font-size:1.5rem; border-bottom:1px solid var(--line); padding-bottom:.35rem; }
p,ul,ol,pre,blockquote,table { margin:0 0 1.15rem; }
code { background:var(--code); padding:.12em .35em; border-radius:.25rem; font-size:.9em; }
pre { background:var(--code); padding:1rem; border-radius:.5rem; overflow-x:auto; }
pre code { background:none; padding:0; }
blockquote { border-left:3px solid var(--line); margin-left:0; padding-left:1rem; color:var(--muted); }
hr { border:0; border-top:1px solid var(--line); margin:2rem 0; }

.crumbs { font:.82rem ui-sans-serif,system-ui,sans-serif; color:var(--muted); margin-bottom:.5rem; }
.crumbs a { color:var(--muted); text-decoration:none; }
.crumbs a:hover { color:var(--accent); }
.meta { color:var(--muted); font:.85rem ui-sans-serif,system-ui,sans-serif; }
.empty { color:var(--muted); font-style:italic; }
.grid { display:grid; grid-template-columns:repeat(auto-fill,minmax(15rem,1fr)); gap:.9rem; margin-bottom:2rem; }
.card { display:block; padding:.9rem 1rem; border:1px solid var(--line); border-radius:.5rem; background:var(--panel); text-decoration:none; color:var(--text); }
.card:hover { border-color:var(--accent); }
.card strong { display:block; font-family:ui-sans-serif,system-ui,sans-serif; margin-bottom:.25rem; }
.card span { color:var(--muted); font-size:.85rem; }
.badge { display:inline-block; background:var(--accent); color:#fff; border-radius:.3rem; padding:.05rem .45rem; font:600 .7rem ui-sans-serif,system-ui,sans-serif; text-transform:uppercase; letter-spacing:.04em; margin-right:.4rem; vertical-align:.1em; }
.chip { display:inline-block; border:1px solid var(--line); color:var(--muted); border-radius:1rem; padding:.05rem .6rem; font:600 .72rem ui-sans-serif,system-ui,sans-serif; margin-right:.35rem; text-decoration:none; }
a.chip:hover { border-color:var(--accent); color:var(--accent); }
.chip.warn { border-color:var(--warn); color:var(--warn); }

.tablewrap { overflow-x:auto; }
table { border-collapse:collapse; width:100%; font-size:.92rem; }
th,td { border:1px solid var(--line); padding:.45rem .65rem; text-align:left; }
th { background:var(--code); font-family:ui-sans-serif,system-ui,sans-serif; }
mark { background:var(--mark); color:var(--markText); border-radius:.2rem; padding:0 .15em; }

.result { border-bottom:1px solid var(--line); padding-bottom:.5rem; margin-bottom:1.2rem; }
.result h2 { border:0; margin:0 0 .2rem; font-size:1.15rem; }
.result.active { background:var(--code); border-radius:.4rem; padding:.5rem .75rem; }
.filters { display:flex; gap:.5rem; flex-wrap:wrap; align-items:center; margin:1rem 0 2rem; font:ui-sans-serif,system-ui,sans-serif; }
.filters input, .filters select { padding:.35rem .5rem; border:1px solid var(--line); border-radius:.35rem; background:var(--bg); color:var(--text); font:inherit; font-size:.85rem; }
.filters button { padding:.35rem .8rem; border:1px solid var(--line); border-radius:.35rem; background:var(--accent); color:#fff; font:600 .85rem ui-sans-serif,system-ui,sans-serif; cursor:pointer; }

.related { margin-top:3rem; border-top:1px solid var(--line); padding-top:1rem; }
.related h2 { border:0; font-size:1.1rem; margin-top:0; }
.related ul { margin:0; padding-left:1.1rem; }
.related.sources { font-size:.9rem; }

.lane { margin-bottom:2.5rem; }
.lane h2 { display:flex; align-items:baseline; gap:.6rem; }
.lane .count { font:600 .78rem ui-sans-serif,system-ui,sans-serif; color:var(--muted); }
.rows { list-style:none; margin:0; padding:0; }
.rows li { display:flex; justify-content:space-between; gap:1rem; align-items:baseline; padding:.4rem 0; border-bottom:1px solid var(--line); }
.rows li > a { text-decoration:none; font-family:ui-sans-serif,system-ui,sans-serif; font-size:.95rem; }
.rows li > a:hover { text-decoration:underline; }
.rows .when { color:var(--muted); font:.78rem ui-sans-serif,system-ui,sans-serif; white-space:nowrap; }
.rows .why { color:var(--muted); font:.8rem ui-sans-serif,system-ui,sans-serif; }

.tags { display:flex; flex-wrap:wrap; gap:.5rem; }
.tags a { display:inline-block; padding:.3rem .7rem; border:1px solid var(--line); border-radius:1rem; text-decoration:none; color:var(--text); font:ui-sans-serif,system-ui,sans-serif; font-size:.85rem; }
.tags a:hover { border-color:var(--accent); color:var(--accent); }
.tags a b { color:var(--muted); font-weight:600; margin-left:.35rem; font-size:.78rem; }

.graphwrap { overflow:auto; border:1px solid var(--line); border-radius:.5rem; background:var(--panel); }
.graph .edge { stroke:var(--line); stroke-width:1; }
.graph .node circle { fill:var(--accent); opacity:.85; }
.graph .node:hover circle { fill:var(--text); }
.graph .node text { font:11px ui-sans-serif,system-ui,sans-serif; fill:var(--muted); }
.graph .node:hover text { fill:var(--text); }
.graph .ring { fill:none; stroke:var(--line); stroke-dasharray:3 4; }
.graph .topic { font:600 12px ui-sans-serif,system-ui,sans-serif; fill:var(--muted); }

kbd { background:var(--code); border:1px solid var(--line); border-bottom-width:2px; border-radius:.25rem; padding:.05em .4em; font:600 .8em ui-sans-serif,system-ui,sans-serif; }
dialog { border:1px solid var(--line); border-radius:.6rem; background:var(--panel); color:var(--text); padding:1.5rem 2rem; max-width:28rem; }
dialog::backdrop { background:rgba(0,0,0,.45); }
dialog table { font-size:.9rem; } dialog td { border:0; padding:.25rem .5rem .25rem 0; }

footer { max-width:1400px; margin:auto; padding:2rem 1.5rem 3rem; color:var(--muted); font:.8rem ui-sans-serif,system-ui,sans-serif; border-top:1px solid var(--line); }

@media (max-width:900px) {
  .shell { grid-template-columns:minmax(0,1fr); gap:0; }
  aside { position:static; max-height:none; padding:1rem 0 0; border-bottom:1px solid var(--line); }
  .bar { padding:.7rem 1rem; }
  .shell, footer { padding-left:1rem; padding-right:1rem; }
}
`

// wikiJS is served from /assets/wiki.js. Everything it stores stays in the
// viewer's own browser; the server never learns what was read.
const wikiJS = `(function(){
  var root=document.documentElement;
  var THEME="jot-theme", SEEN="jot-recent";

  /* ---- theme -------------------------------------------------------- */
  try { var saved=localStorage.getItem(THEME); if(saved){ root.setAttribute("data-theme",saved); } } catch(e){}
  var themeBtn=document.getElementById("theme");
  if(themeBtn){
    themeBtn.addEventListener("click",function(){
      var explicit=root.getAttribute("data-theme");
      var dark = explicit ? explicit==="dark"
        : window.matchMedia("(prefers-color-scheme: dark)").matches;
      var next = dark ? "light" : "dark";
      root.setAttribute("data-theme",next);
      try { localStorage.setItem(THEME,next); } catch(e){}
    });
  }

  /* ---- recently viewed (this browser only) --------------------------- */
  function readSeen(){
    try { return JSON.parse(localStorage.getItem(SEEN)||"[]"); } catch(e){ return []; }
  }
  var page=document.body.getAttribute("data-page"), title=document.body.getAttribute("data-title");
  if(page){
    try{
      var list=readSeen().filter(function(e){ return e && e.id!==page; });
      list.unshift({id:page,title:title||page,at:Date.now()});
      localStorage.setItem(SEEN, JSON.stringify(list.slice(0,25)));
    }catch(e){}
  }
  var seenBox=document.getElementById("recently-viewed");
  if(seenBox){
    var seen=readSeen().filter(function(e){ return e && e.id!==page; }).slice(0,8);
    if(seen.length){
      var ul=document.createElement("ul");
      seen.forEach(function(e){
        var li=document.createElement("li"), a=document.createElement("a");
        a.href="/wiki/"+e.id.split("/").map(encodeURIComponent).join("/");
        a.textContent=e.title; li.appendChild(a); ul.appendChild(li);
      });
      seenBox.appendChild(ul);
    } else {
      seenBox.remove();
    }
  }

  /* ---- live search --------------------------------------------------- */
  var input=document.querySelector(".search input[name=q]");
  var box=null, timer=null, items=[], cursor=-1;
  function closeBox(){ if(box){ box.remove(); box=null; } items=[]; cursor=-1; }
  function openBox(hits){
    closeBox();
    if(!hits.length) return;
    box=document.createElement("div"); box.className="suggest";
    hits.forEach(function(h){
      var a=document.createElement("a");
      a.href="/wiki/"+h.id.split("/").map(encodeURIComponent).join("/");
      var s=document.createElement("strong"); s.textContent=h.title; a.appendChild(s);
      var d=document.createElement("span"); d.textContent=h.heading||h.description||h.type||""; a.appendChild(d);
      box.appendChild(a);
    });
    input.parentNode.appendChild(box);
    items=Array.prototype.slice.call(box.querySelectorAll("a"));
  }
  function moveCursor(delta){
    if(!items.length) return;
    if(cursor>=0) items[cursor].classList.remove("active");
    cursor=(cursor+delta+items.length)%items.length;
    items[cursor].classList.add("active");
    items[cursor].scrollIntoView({block:"nearest"});
  }
  if(input){
    input.addEventListener("input",function(){
      clearTimeout(timer);
      var q=input.value.trim();
      if(q.length<2){ closeBox(); return; }
      timer=setTimeout(function(){
        fetch("/api/search?q="+encodeURIComponent(q)+"&limit=8",{headers:{"Accept":"application/json"}})
          .then(function(r){ return r.ok?r.json():{hits:[]}; })
          .then(function(d){ openBox(d.hits||[]); })
          .catch(function(){ closeBox(); });
      },140);
    });
    input.addEventListener("keydown",function(e){
      if(e.key==="ArrowDown"){ e.preventDefault(); moveCursor(1); }
      else if(e.key==="ArrowUp"){ e.preventDefault(); moveCursor(-1); }
      else if(e.key==="Enter" && cursor>=0){ e.preventDefault(); window.location=items[cursor].href; }
      else if(e.key==="Escape"){ closeBox(); input.blur(); }
    });
    document.addEventListener("click",function(e){
      if(box && !input.parentNode.contains(e.target)) closeBox();
    });
  }

  /* ---- keyboard navigation ------------------------------------------- */
  var chord=null, chordTimer=null;
  var results=Array.prototype.slice.call(document.querySelectorAll(".result"));
  var rIndex=-1;
  function focusResult(delta){
    if(!results.length) return;
    if(rIndex>=0) results[rIndex].classList.remove("active");
    rIndex=Math.max(0,Math.min(results.length-1,rIndex+delta));
    results[rIndex].classList.add("active");
    results[rIndex].scrollIntoView({block:"center",behavior:"smooth"});
  }
  function typing(e){
    var t=e.target, n=t.tagName;
    return n==="INPUT"||n==="TEXTAREA"||n==="SELECT"||t.isContentEditable;
  }
  document.addEventListener("keydown",function(e){
    if(typing(e)||e.metaKey||e.ctrlKey||e.altKey) return;
    var help=document.getElementById("help");
    if(e.key==="?"){ e.preventDefault(); if(help){ help.open?help.close():help.showModal(); } return; }
    if(e.key==="Escape" && help && help.open){ help.close(); return; }
    if(e.key==="/"){ e.preventDefault(); if(input){ input.focus(); input.select(); } return; }
    if(chord==="g"){
      chord=null; clearTimeout(chordTimer);
      var to={h:"/",l:"/log",t:"/tags",e:"/loose-ends",r:"/random",n:"/recent",a:"/graph"}[e.key];
      if(to){ e.preventDefault(); window.location=to; }
      return;
    }
    if(e.key==="g"){ chord="g"; clearTimeout(chordTimer); chordTimer=setTimeout(function(){chord=null;},900); return; }
    if(e.key==="j"){ e.preventDefault(); focusResult(rIndex<0?0:1); return; }
    if(e.key==="k"){ e.preventDefault(); focusResult(-1); return; }
    if(e.key==="Enter" && rIndex>=0){
      var link=results[rIndex].querySelector("a");
      if(link){ e.preventDefault(); window.location=link.href; }
    }
  });
})();`
