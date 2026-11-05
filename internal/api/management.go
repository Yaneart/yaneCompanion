package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"yanecompanion/internal/config"
)

type Management struct {
	mu      sync.Mutex
	proof   [32]byte
	expires time.Time
}

func (m *Management) open() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var data [32]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(data[:])
	m.proof, m.expires = sha256.Sum256([]byte(token)), time.Now().Add(5*time.Minute)
	return token, nil
}

func (m *Management) valid(token string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	proof := sha256.Sum256([]byte(token))
	return len(token) == 64 && time.Now().Before(m.expires) && subtle.ConstantTimeCompare(proof[:], m.proof[:]) == 1
}

func (h Handler) manage(w http.ResponseWriter, r *http.Request) {
	self := "http://" + config.Address
	if h.Management == nil || h.Pairings == nil || len(r.Header.Values("Origin")) != 1 || r.Header.Get("Origin") != self || !h.Management.valid(bearer(r)) {
		fail(w, 403, "MANAGEMENT_AUTH_REQUIRED")
		return
	}
	if r.Method != "POST" {
		fail(w, 404, "NOT_FOUND")
		return
	}
	switch r.URL.Path {
	case "/local/manage/snapshot":
		var body struct{}
		if !jsonBody(w, r, &body) {
			return
		}
		requests, grants := h.Pairings.Snapshot()
		respond(w, 200, map[string]any{"requests": requests, "grants": grants})
	case "/local/manage/decision":
		var body struct {
			RequestID string `json:"requestId"`
			Code      string `json:"code"`
			Approve   *bool  `json:"approve"`
		}
		if !jsonBody(w, r, &body) {
			return
		}
		if body.Approve == nil {
			fail(w, 400, "INVALID_REQUEST")
			return
		}
		if err := h.Pairings.Decide(body.RequestID, body.Code, *body.Approve); err != nil {
			pairError(w, err)
			return
		}
		respond(w, 204, nil)
	case "/local/manage/revoke":
		var body struct {
			ID string `json:"id"`
		}
		if !jsonBody(w, r, &body) {
			return
		}
		if body.ID == "" {
			fail(w, 400, "INVALID_REQUEST")
			return
		}
		if err := h.Pairings.Revoke(body.ID); err != nil {
			pairError(w, err)
			return
		}
		respond(w, 204, nil)
	default:
		fail(w, 404, "NOT_FOUND")
	}
}

func managementPage(w http.ResponseWriter) {
	hash := sha256.Sum256([]byte(managementScript))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'sha256-"+base64.StdEncoding.EncodeToString(hash[:])+"'; style-src 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	w.Header().Set("X-Frame-Options", "DENY")
	_, _ = w.Write([]byte(managementHTML + "<script>" + managementScript + "</script>"))
}

const managementHTML = `<!doctype html><html lang="ru"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>yaneCompanion</title><style>body{font:16px system-ui;max-width:640px;margin:50px auto;padding:20px;background:#111827;color:#e5e7eb}h1{font-size:26px}section{border:1px solid #475569;border-radius:12px;padding:20px;margin:16px 0}button{font:inherit;padding:10px 16px;margin:8px 8px 0 0;cursor:pointer;border-radius:8px;border:0}strong{display:block;font-size:24px;letter-spacing:4px;margin:15px 0}p{overflow-wrap:anywhere}small{color:#cbd5e1}</style><h1>yaneCompanion</h1><p>Сравните адрес сайта и код с запросом в браузере. Разрешайте доступ только знакомому сайту.</p><p id="status" role="status">Проверяем локальный доступ…</p><main id="requests"></main><h2>Разрешённые подключения</h2><div id="grants"></div>`

const managementScript = `(() => {
  addEventListener('hashchange', () => { if (location.hash) location.reload(); });
  const token = location.hash.slice(1);
  history.replaceState(null, '', location.pathname);
  const status = document.getElementById('status');
  let busy = false;
  async function call(path, body = {}) {
    const response = await fetch('/local/manage/' + path, {method:'POST', headers:{'Authorization':'Bearer ' + token,'Content-Type':'application/json'},body:JSON.stringify(body),credentials:'omit',redirect:'error',cache:'no-store'});
    if (!response.ok) throw new Error('Локальный доступ истёк или запрос уже завершён. Откройте окно командой yanecompanion manage.');
    return response.status === 204 ? null : response.json();
  }
  function text(tag, value) { const el = document.createElement(tag); el.textContent = value; return el; }
  function action(label, fn) { const button = text('button',label); button.onclick = async () => { if(busy)return; busy=true;button.disabled=true; try {await fn();await refresh();} catch(error){status.textContent=error.message;} finally {busy=false;button.disabled=false;} }; return button; }
  async function refresh() {
    try {
      const snapshot = await call('snapshot');
      status.textContent = snapshot.requests.length ? 'Ожидается ваше решение' : 'Нет запросов на подключение';
      const requests = document.getElementById('requests'); requests.replaceChildren();
      for (const request of snapshot.requests) {
        const section = document.createElement('section');
        section.append(text('p',request.origin),text('strong',request.code),text('small','Запрос действует до ' + new Date(request.expiresAt).toLocaleTimeString()));
        for(const [label,approve] of [['Разрешить',true],['Отклонить',false]]) section.append(action(label,()=>call('decision',{requestId:request.requestId,code:request.code,approve})));
        requests.append(section);
      }
      const grants = document.getElementById('grants'); grants.replaceChildren();
      if(!snapshot.grants.length)grants.append(text('p','Нет разрешённых подключений'));
      for(const grant of snapshot.grants){const section=document.createElement('section');section.append(text('p',grant.origin),action('Отозвать доступ',()=>call('revoke',{id:grant.id})));grants.append(section);}
    } catch(error) { status.textContent=error.message; clearInterval(timer); }
  }
  const timer = setInterval(()=>{if(!busy)void refresh();},2000);
  void refresh();
})();`
