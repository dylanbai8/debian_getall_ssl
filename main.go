package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type SystemConfig struct {
	WebEnable bool   `json:"web_enable"`
	WebUser   string `json:"web_user"`
	WebPass   string `json:"web_pass"`
	Listen    string `json:"listen"`
	CronHours int    `json:"cron_hours"`
}

type IPConfig struct {
	Enable       bool     `json:"enable"`
	IPAddr       string   `json:"ip_addr"`
	Webroot      string   `json:"webroot"`
	Email        string   `json:"email"`
	RenewDays    int      `json:"renew_days"`
	InstallPaths []string `json:"install_paths"`
}

type Domain struct {
	Domain      string `json:"domain"`
	Webroot     string `json:"webroot"`
	InstallPath string `json:"install_path"`
}

type DomainConfig struct {
	Enable    bool     `json:"enable"`
	Email     string   `json:"email"`
	RenewDays int      `json:"renew_days"`
	Domains   []Domain `json:"domains"`
}

var (
	sysCfg    SystemConfig
	ipCfg     IPConfig
	domainCfg DomainConfig
	basePath  string
	logFile   *os.File
)

func safePath(p string) string { return filepath.Join(basePath, p) }

func initBasePath() {
	exe, _ := os.Executable()
	basePath = filepath.Dir(exe)
}

func acmeInstalled() bool {
	_, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".acme.sh/acme.sh"))
	return err == nil
}

func installAcme(email string) {
	if acmeInstalled() {
		log.Println("[INFO] acme.sh 已安装")
		return
	}
	logStep("安装 acme.sh", func() error {
		return run("sh", "-c", "curl https://get.acme.sh | sh -s email="+email)
	})
}

func initFiles() {
	os.MkdirAll(safePath("config"), 0755)
	os.MkdirAll(safePath("web"), 0755)
	os.MkdirAll(safePath("logs"), 0755)

	writeIfNotExist("config/system.json", `{"web_enable":true,"web_user":"admin","web_pass":"123456","listen":":8089","cron_hours":6}`)
	writeIfNotExist("config/ip.json", `{"enable":true,"ip_addr":"166.108.238.105","webroot":"/www/wwwroot/166.108.238.105","email":"example@qq.com","renew_days":3,"install_paths":["/www/server/panel/vhost/cert/166.108.238.105"]}`)
	writeIfNotExist("config/domain.json", `{"enable":true,"email":"example@qq.com","renew_days":60,"domains":[{"domain":"example.com","webroot":"/www/wwwroot/example.com","install_path":"/www/server/panel/vhost/cert/example.com"}]}`)
	writeIfNotExist("web/index.html", defaultHTML)
}

func writeIfNotExist(p, c string) {
	if _, err := os.Stat(safePath(p)); os.IsNotExist(err) {
		os.WriteFile(safePath(p), []byte(c), 0644)
	}
}

func initLog() {
	lp := safePath("logs/cert-manager.log")
	if fi, err := os.Stat(lp); err == nil && time.Since(fi.ModTime()) > 30*24*time.Hour {
		os.Remove(lp)
	}
	f, _ := os.OpenFile(lp, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	log.SetOutput(f)
	logFile = f
}

func logStep(n string, fn func() error) {
	start := time.Now()
	log.Printf("[开始] ==>> %s 开始\n", n)
	err := fn()
	if err != nil {
		log.Printf("[失败] ==>> %s 失败 (%s): %v\n", n, time.Since(start), err)
	} else {
		log.Printf("[成功] ==>> %s 成功 (%s)\n", n, time.Since(start))
	}
}

func loadJSON(p string, v any) {
	b, _ := os.ReadFile(p)
	json.Unmarshal(b, v)
}

func saveJSON(p string, b []byte) { os.WriteFile(p, b, 0644) }

func auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !sysCfg.WebEnable {
			w.WriteHeader(403)
			w.Write([]byte("Web管理已关闭"))
			return
		}
		raw := r.Header.Get("Authorization")
		if raw == "" {
			w.Header().Set("WWW-Authenticate", `Basic realm="cert-manager"`)
			w.WriteHeader(401)
			w.Write([]byte("需要登录"))
			return
		}
		d, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(raw, "Basic "))
		p := strings.SplitN(string(d), ":", 2)
		if len(p) != 2 || p[0] != sysCfg.WebUser || p[1] != sysCfg.WebPass {
			w.WriteHeader(403)
			w.Write([]byte("账号密码错误"))
			return
		}
		h(w, r)
	}
}

func run(cmd string, args ...string) error {
	c := exec.Command(cmd, args...)
	c.Stdout = logFile
	c.Stderr = logFile
	return c.Run()
}

func getPublicIP() string {
	resp, err := http.Get("https://api.ipify.org?format=text")
	if err != nil {
		log.Println("获取外网IP失败:", err)
		return "127.0.0.1"
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return strings.TrimSpace(string(b))
}

func killOld() {
	out, _ := exec.Command("pgrep", "-f", os.Args[0]).Output()
	for _, pid := range strings.Fields(string(out)) {
		if pid != fmt.Sprint(os.Getpid()) {
			syscall.Kill(toInt(pid), syscall.SIGTERM)
		}
	}
}

func toInt(s string) int {
	i, _ := strconv.Atoi(s)
	return i
}

func issueIP() {
	if !ipCfg.Enable {
		return
	}
	defer recoverLog("IP")

	log.Println("\n\n\n\n============ [开始]签发 IP证书 ============")
	installAcme(ipCfg.Email)
	run(filepath.Join(os.Getenv("HOME"), ".acme.sh/acme.sh"), "--set-default-ca", "--server", "letsencrypt")

	logStep("申请证书 ", func() error {
		return run(filepath.Join(os.Getenv("HOME"), ".acme.sh/acme.sh"),
			   "--issue", "--server", "letsencrypt",
	     "--certificate-profile", "shortlived",
	     "--days", strconv.Itoa(ipCfg.RenewDays),
			   "-d", ipCfg.IPAddr, "-w", ipCfg.Webroot)
	})

	for _, p := range ipCfg.InstallPaths {
		p := p
		logStep("安装证书 "+p, func() error {
			os.MkdirAll(p, 0755)
			return run(filepath.Join(os.Getenv("HOME"), ".acme.sh/acme.sh"),
				   "--install-cert", "-d", ipCfg.IPAddr,
	      "--key-file", p+"/privkey.pem",
	      "--fullchain-file", p+"/fullchain.pem")
		})
	}

	run("nginx", "-t")
	run("nginx", "-s", "reload")
	log.Println("\n============ [完成]签发 IP证书 ============\n\n\n")
}

func issueDomain() {
	if !domainCfg.Enable {
		return
	}
	defer recoverLog("DOMAIN")

	log.Println("\n\n\n\n============ [开始]签发 域名证书 ============")
	installAcme(domainCfg.Email)
	run(filepath.Join(os.Getenv("HOME"), ".acme.sh/acme.sh"), "--set-default-ca", "--server", "letsencrypt")

	for _, d := range domainCfg.Domains {
		d := d
		logStep("申请证书 "+d.Domain, func() error {
			return run(filepath.Join(os.Getenv("HOME"), ".acme.sh/acme.sh"),
				   "--issue", "--server", "letsencrypt",
	      "--days", strconv.Itoa(domainCfg.RenewDays),
				   "-d", d.Domain, "-w", d.Webroot)
		})
		logStep("安装证书 "+d.Domain, func() error {
			os.MkdirAll(d.InstallPath, 0755)
			return run(filepath.Join(os.Getenv("HOME"), ".acme.sh/acme.sh"),
				   "--install-cert", "-d", d.Domain,
	      "--key-file", d.InstallPath+"/privkey.pem",
	      "--fullchain-file", d.InstallPath+"/fullchain.pem")
		})
	}

	run("nginx", "-t")
	run("nginx", "-s", "reload")
	log.Println("\n============ [完成]签发 域名证书 ============\n\n\n")
}

func recoverLog(tag string) {
	if r := recover(); r != nil {
		log.Println("["+tag+" PANIC]", r, string(debug.Stack()))
	}
}

func main() {
	killOld()
	initBasePath()
	initFiles()
	initLog()

	loadJSON(safePath("config/system.json"), &sysCfg)
	loadJSON(safePath("config/ip.json"), &ipCfg)
	loadJSON(safePath("config/domain.json"), &domainCfg)

	fmt.Println("配置文件：" + safePath("config"))
	fmt.Println("后台管理：http://" + getPublicIP() + sysCfg.Listen)

	home := os.Getenv("HOME")
	if home == "" {
		home = "/root"
		os.Setenv("HOME", home)
	}

	go func() {
		t := time.NewTicker(time.Duration(sysCfg.CronHours) * time.Hour)
		for range t.C {
			log.Println("\n\n+++++++++++++++ 定时任务触发 +++++++++++++++\n\n")
			go issueIP()
			go issueDomain()
		}
	}()

	mux := http.NewServeMux()
	mux.Handle("/", auth(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, safePath("web/index.html"))
	}))
	mux.HandleFunc("/api/system", apiHandler(&sysCfg, safePath("config/system.json")))
	mux.HandleFunc("/api/ip", apiHandler(&ipCfg, safePath("config/ip.json")))
	mux.HandleFunc("/api/domain", apiHandler(&domainCfg, safePath("config/domain.json")))
	mux.HandleFunc("/api/ip/issue", auth(func(w http.ResponseWriter, r *http.Request) { go issueIP(); w.Write([]byte("ok")) }))
	mux.HandleFunc("/api/domain/issue", auth(func(w http.ResponseWriter, r *http.Request) { go issueDomain(); w.Write([]byte("ok")) }))

	log.Println("服务启动", sysCfg.Listen, "\n\n\n\n")
	http.ListenAndServe(sysCfg.Listen, mux)
}

func apiHandler(target any, path string) http.HandlerFunc {
	return auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(target)
		} else {
			b, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(b, target); err != nil {
				w.WriteHeader(400)
				w.Write([]byte("JSON 错误：" + err.Error()))
				return
			}
			saveJSON(path, b)
			w.Write([]byte("ok"))
		}
	})
}

const defaultHTML = `
<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>证书管理</title>
<style>
body{font-family:Arial;background:#f2f3f7;padding:20px}
.card{background:#fff;padding:10px;margin-bottom:12px;border-radius:6px}
.header{cursor:pointer;font-weight:bold}
.content{display:block;margin-top:8px}
textarea{width:100%;height:140px}
#msg{position:fixed;top:10px;right:10px;padding:10px;border-radius:4px;display:none;color:#fff}
</style>
</head>
<body>

<div id="msg"></div>
<p><b>&nbsp;&nbsp;证书签发管理 (点击标题可折叠)</b></p>

<div class="card">
<div class="header" onclick="toggle(this)">系统设置</div>
<div class="content"><textarea id="system"></textarea><br><button onclick="save('system')">保存</button></div>
</div>

<div class="card">
<div class="header" onclick="toggle(this)">IP证书 支持单IP签发多路径安装</div>
<div class="content"><textarea id="ip"></textarea><br><button onclick="save('ip')">保存</button><button onclick="issue('ip')">签发</button></div>
</div>

<div class="card">
<div class="header" onclick="toggle(this)">域名证书 支持多域名签发安装</div>
<div class="content"><textarea id="domain"></textarea><br><button onclick="save('domain')">保存</button><button onclick="issue('domain')">签发</button></div>
</div>

<script>
function toggle(h){
let c=h.nextElementSibling;
c.style.display = c.style.display=='none'?'block':'none'
}
function flash(t,c){
let m=document.getElementById('msg');
m.style.background=c; m.innerText=t; m.style.display='block';
setTimeout(()=>m.style.display='none',1000)
}
['system','ip','domain'].forEach(n=>{
fetch('/api/'+n).then(r=>r.json()).then(j=>{
document.getElementById(n).value=JSON.stringify(j,null,2)
})
})
function save(n){
let v=document.getElementById(n).value;
try{ JSON.parse(v) }catch(e){ flash('JSON 格式错误','red'); return }
fetch('/api/'+n,{method:'POST',body:v}).then(r=>r.text()).then(t=>{
t=='ok'?flash('保存成功','#2ecc71'):flash(t,'red')
})
}
function issue(n){
fetch('/api/'+n+'/issue',{method:'POST'}).then(()=>flash('任务已提交','#3498db'))
}
</script>
</body>
</html>
`
