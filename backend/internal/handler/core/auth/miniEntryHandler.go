package auth

import (
	"backend/internal/svc"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
)

// Static bridge: the one-time credential stays in the fragment, outside HTTP
// URLs/access logs. Clear it before exchanging and never render server error text.
func MiniEntryHandler(_ *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		bytes := make([]byte, 18)
		if _, err := rand.Read(bytes); err != nil {
			http.Error(w, "unavailable", 503)
			return
		}
		nonce := base64.RawStdEncoding.EncodeToString(bytes)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+nonce+"'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'")
		fmt.Fprintf(w, miniEntryHTML, nonce)
	}
}

const miniEntryHTML = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Tier0 Enterprise</title><body><p id="status">正在打开应用…</p><script nonce="%s">
(function(){
var en = (navigator.language || '').toLowerCase().indexOf('zh') !== 0;
var status = document.getElementById('status');
status.textContent = en ? 'Opening app…' : '正在打开应用…';
var ticket = location.hash.slice(1);
history.replaceState(null, '', location.pathname);
function failed(){ status.textContent = en ? 'This entry expired or access was denied. Return to the mini program and open the app again.' : '入口已过期或没有访问权限，请返回小程序重新打开应用。'; }
if(!/^[A-Za-z0-9_-]{43}$/.test(ticket)){failed();return;}
fetch('/api/core/auth/mini-entry-exchange',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'},body:JSON.stringify({ticket:ticket})}).then(function(response){if(!response.ok)throw new Error();return response.json();}).then(function(body){var path=body && body.data && body.data.redirect;if(body.code!==200 || !/^\/mini-app\/\d+\/\d+$/.test(path))throw new Error();location.replace(path);}).catch(failed);
})();</script></body></html>`
