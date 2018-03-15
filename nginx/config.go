package nginx

import (
	"crypto/x509"
	"encoding/pem"
	"github.com/facebookgo/pidfile"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"os"
	"strings"
	"syscall"
	"text/template"
)

var nginxConfTmpl, upstreamTmpl, serverTmpl *template.Template
var certs map[string]*x509.Certificate

func init() {
	var err error
	upstreamTmpl, err = template.New("upstream").Parse(`
{{ range $name, $uri := $.Upstreams -}}
upstream {{ $name }} {
    # fake server otherwise ngx_http_upstream will report error when startup
    server 127.0.0.1:11111;
    
    # all backend server will pull from consul when startup and will delete fake server
	upsync {{ $.ConsulAddr }}/v1/kv/{{ $.ConsulPrefix }}{{ $name }}/ upsync_timeout=6m upsync_interval=500ms upsync_type=consul strong_dependency=off;
	upsync_dump_path /usr/local/openresty/nginx/upstreams/{{ $name }}.upstream;
{{ if $uri }}
	check interval=3000 rise=2 fall=5 timeout=1000 type=http;
	check_http_send "GET {{ $uri }} HTTP/1.0\r\n\r\n";
	check_http_expect_alive http_2xx http_3xx;
{{ end -}}
}

{{ end -}}`)
	if err != nil {
		log.Fatal(err)
	}

	serverTmpl, err = template.New("server").Parse(`
{{ range $serverName, $server := $.Servers -}}
server {
    listen  80;
    {{ if $server.SSL -}}
    listen 443;
    {{ end -}}
    server_name  .{{ $serverName }};
    {{ if $server.SSL -}}
    ssl_certificate {{ $.NginxPath }}/ssl/{{ $server.SSL }}.crt;
    ssl_certificate_key {{ $.NginxPath }}/ssl/{{ $server.SSL }}.key;
    ssl_protocols TLSv1 TLSv1.1 TLSv1.2;
    ssl_ciphers  HIGH:!aNULL:!MD5;
    {{ end -}}
    include proxy.conf;
{{ range $uri, $location := $server.Locations -}}
{{ if eq $uri "/" }}
    location / {
        proxy_pass  http://{{ $location.Upstream }};
        access_log  {{ $.LogPath }}{{ $serverName }}___{{ $location.Upstream }}.access.log  main;
    }
{{ else }}
    location /{{ $uri }}/ {
        rewrite /{{ $uri }}/(.*) /$1 break;
        proxy_pass  http://{{ $location.Upstream }};
        access_log  {{ $.LogPath }}{{ $serverName }}___{{ $location.Upstream }}.access.log  main;
    }
{{ end -}}
{{ end -}}
}

{{ end -}}`)
	if err != nil {
		log.Fatal(err)
	}

	nginxConfTmpl, err = template.New("nginx").Parse(`
worker_processes  4;

error_log  {{ $.LogPath }}error.log warn;
pid        {{ $.PidPath }};

events {
    worker_connections  1024;
}

http {
    include       mime.types;
    default_type  application/octet-stream;

    log_format  main  '$remote_addr - $remote_user [$time_local] "$request" '
                      '$status $body_bytes_sent "$http_referer" '
                      '"$http_user_agent" "$http_x_forwarded_for"';

    access_log  {{ $.LogPath }}access.log  main;

    sendfile        on;

    keepalive_timeout  65;

    server_names_hash_max_size 1024;

    server {
        listen 80;
        server_name  .{{ $.ServerName }};
        location / {
            upstream_show;
        }
    }

    include {{ $.NginxPath }}conf/server.conf;
    include {{ $.NginxPath }}conf/upstream.conf;
}`)
	if err != nil {
		log.Fatal(err)
	}

	certs = make(map[string]*x509.Certificate)
}

type Location struct {
	Upstream  string
	HttpsOnly bool
}

type Server struct {
	SSL       string
	Locations map[string]Location
}

type Config struct {
	Servers   map[string]Server
	Upstreams map[string]string
}

func Init(nginxPath string, logPath string, serverName string, pidPath string) error {
	if err := rendorNginxConf(nginxPath, logPath, pidPath, serverName); err != nil {
		return err
	}
	if err := loadCrt(nginxPath); err != nil {
		return err
	}
	if _, err := os.Create(nginxPath + "conf/server.conf"); err != nil {
		return err
	}
	if _, err := os.Create(nginxPath + "conf/upstream.conf"); err != nil {
		return err
	}
	_, err := os.Stat(nginxPath + "upstreams")
	if os.IsNotExist(err) {
		if err := os.Mkdir(nginxPath+"upstreams", os.ModePerm); err != nil {
			return err
		}
	} else {
		return err
	}
	if _, err := os.Create(nginxPath + "lock"); err != nil {
		return err
	}
	return nil
}

func rendorNginxConf(nginxPath string, logPath string, pidPath string, serverName string) error {
	f, err := os.Create(nginxPath + "conf/nginx.conf")
	if err != nil {
		return err
	}
	err = nginxConfTmpl.Execute(f, map[string]interface{}{
		"NginxPath":  nginxPath,
		"LogPath":    logPath,
		"PidPath":    pidPath,
		"ServerName": serverName,
	})
	if err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func renderServerConf(config *Config, nginxPath string, logPath string) error {
	f, err := os.Create(nginxPath + "conf/server.conf")
	if err != nil {
		return err
	}
	err = serverTmpl.Execute(f, map[string]interface{}{
		"NginxPath": nginxPath,
		"LogPath":   logPath,
		"Servers":   config.Servers,
	})
	if err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func renderUpstreamConf(config *Config, consulAddr string, consulPrefix string, nginxPath string) error {
	f, err := os.Create(nginxPath + "conf/upstream.conf")
	if err != nil {
		return err
	}
	err = upstreamTmpl.Execute(f, map[string]interface{}{
		"ConsulAddr":   consulAddr,
		"ConsulPrefix": consulPrefix,
		"Upstreams":    config.Upstreams,
	})
	if err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func loadCrt(nginxPath string) error {
	files, err := ioutil.ReadDir(nginxPath + "ssl")
	if err != nil {
		return err
	}
	for _, file := range files {
		if strings.Contains(file.Name(), ".crt") {
			bytes, err := ioutil.ReadFile(nginxPath + "ssl" + file.Name())
			if err != nil {
				return err
			}
			block, _ := pem.Decode(bytes)
			c, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return err
			}
			certs[file.Name()[0:strings.Index(file.Name(), ".crt")]] = c
		}
	}
	return nil
}

func matchSSL(config *Config) {
	for serverName := range config.Servers {
		for certName, cert := range certs {
			err := cert.VerifyHostname(serverName)
			if err == nil {
				v := config.Servers[serverName]
				v.SSL = certName
				config.Servers[serverName] = v
				break
			}
		}
	}
}

func render(config *Config, consulAddr string, consulPrefix string, nginxPath string, logPath string) error {
	if err := renderServerConf(config, nginxPath, logPath); err != nil {
		return err
	}
	if err := renderUpstreamConf(config, consulAddr, consulPrefix, nginxPath); err != nil {
		return err
	}
	return nil
}

func reload(path string) error {
	pidfile.SetPidfilePath(path)
	pid, err := pidfile.Read()
	if err != nil {
		return err
	}
	err = syscall.Kill(pid, syscall.SIGHUP)
	if err != nil {
		return err
	}
	return nil
}

func Reload(config *Config, consulAddr string, consulPrefix, nginxPath string, pidPath string, logPath string) error {

	matchSSL(config)

	if err := render(config, consulAddr, consulPrefix, nginxPath, logPath); err != nil {
		return err
	}

	if err := reload(pidPath); err != nil {
		return err
	}

	return nil
}
