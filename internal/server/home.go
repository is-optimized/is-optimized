package server

import (
	"github.com/is-optimized/is-optimized/internal/render"
	"html/template"
	"net/http"
)

var homeTemplate = template.Must(template.New("home").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>is-optimized.com — Is your website ready?</title><style>{{.CSS}}</style></head><body><main><header>is-optimized.com · A product of Sire Run, Inc.</header><h1>Be easier to find.<br>Be easier to understand.</h1><p>A free website check for search engines, generative search, and AI agents.</p><form id="scan"><label for="domain">Your website domain</label><input id="domain" name="domain" required placeholder="example.com" autocomplete="url" maxlength="253"><button id="scanButton">Check my website</button></form><p class="muted">Homepage + up to four linked pages. No account required.</p><p id="status" role="status" aria-live="polite"></p><div id="results" hidden><div id="scores" class="scores"></div><section><h2>Your full report is ready</h2><p>Evidence, practical fixes, and instructions for your coding agent.</p>{{if .SMS}}<label><input id="consent" type="checkbox"> {{.Disclosure}}</label><p class="muted">Marketing consent is optional. You can view your full report without it.</p><button id="text">Text me the report</button>{{else}}<p class="muted">Text delivery is not enabled yet.</p>{{end}} <a id="view" class="button secondary">View report online</a><div id="handoff" hidden><p>Send this message from your phone to request your PDF link.</p><a id="compose" class="button">Open text message</a><p id="manual"></p><pre id="message"></pre></div></section></div><footer><p class="muted">Technical signals, not promises of rankings or AI citations. Your report can be shared publicly.</p></footer></main><script>
const statusEl=document.getElementById('status'); let reportID='';
async function request(path,body){const response=await fetch(path,body?{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)}:{});if(!response.ok)throw Error(await response.text());return response.json()}
function show(report){reportID=report.ID||report.id;document.getElementById('scores').replaceChildren();for(const p of (report.Pillars||report.pillars)){const el=document.createElement('section');const h=document.createElement('h2');h.textContent=p.name||p.Name;const v=document.createElement('p');v.className='score';const n=p.score??p.Score;v.textContent=n==null?'Insufficient evidence':Math.round(n)+' / 100';const c=document.createElement('p');c.textContent='Evidence coverage: '+Math.round((p.coverage??p.Coverage)*100)+'%';el.append(h,v,c);document.getElementById('scores').append(el)}document.getElementById('view').href='/'+encodeURIComponent(report.domain||report.Domain);document.getElementById('results').hidden=false;statusEl.textContent='Your report is ready.'}
document.getElementById('scan').onsubmit=async e=>{e.preventDefault();const b=document.getElementById('scanButton');b.disabled=true;document.getElementById('results').hidden=true;document.getElementById('handoff').hidden=true;statusEl.textContent='Checking your website…';try{let data=await request('/api/v1/scans',{domain:document.getElementById('domain').value});for(let tries=0;!data.report&&tries<65;tries++){if(data.state==='failed')throw Error(data.error||'Unable to complete this report.');await new Promise(r=>setTimeout(r,1000));data=await request('/api/v1/scans/'+data.id)}if(!data.report)throw Error('The scan is still running. Please try again shortly.');show(data.report)}catch(err){statusEl.textContent=err.message}finally{b.disabled=false}};
const text=document.getElementById('text');if(text)text.onclick=async()=>{text.disabled=true;try{const d=await request('/api/v1/report-requests',{report_id:reportID,marketing:document.getElementById('consent').checked});document.getElementById('compose').href=d.sms_url;document.getElementById('manual').textContent='Or send this message to '+d.to;document.getElementById('message').textContent=d.body;document.getElementById('handoff').hidden=false}catch(err){statusEl.textContent=err.message}finally{text.disabled=false}};
</script></body></html>`))

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	homeTemplate.Execute(w, struct {
		CSS        template.CSS
		SMS        bool
		Disclosure string
	}{template.CSS(render.Style), s.cfg.SMS, s.cfg.Disclosure})
}
