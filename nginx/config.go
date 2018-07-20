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

var nginxConfTmpl, upstreamTmpl, serverTmpl, proxyConfTmpl *template.Template
var certs map[string]*x509.Certificate

type Location struct {
	Upstream  string
	HttpsOnly bool
	ABTest    bool
}

type Server struct {
	SSL       string
	Locations map[string]Location
}

type Upstream struct {
	HealthCheck string
	Servers     []string
}

type Config struct {
	Servers   map[string]Server
	Upstreams map[string]Upstream
	Err       error
}

type RedisConf struct {
	Sentinel         string
	MasterName       string
	Role             string
	Password         string
	ConnectTimeout   int
	ReadTimeout      int
	DBID             int
	PoolSize         int
	KeepaliveTimeout int
}

type InitConf struct {
	NginxPath                 string
	LogPath                   string
	ServerName                string
	PidPath                   string
	HTTPS                     bool
	SSLPath                   string
	ServerNamesHashMaxSize    int
	ServerNamesHashBucketSize int
	CheckShmSize              int
	ABTest                    bool
	RedisConf                 RedisConf
}

type NginxConf struct {
	NginxPath                 string
	LogPath                   string
	ServerName                string
	PidPath                   string
	ServerNamesHashMaxSize    int
	ServerNamesHashBucketSize int
	CheckShmSize              int
	ABTest                    bool
	RedisConf                 RedisConf
}

type ProxyConf struct {
	NginxPath string
	ABTest    bool
	RedisConf RedisConf
}

type RenderConf struct {
	NginxPath    string
	LogPath      string
	HTTPS        bool
	SSLPath      string
	ConsulAddr   string
	ConsulPrefix string
	ABTest       bool
	RedisConf    RedisConf
}

type ServerConf struct {
	NginxPath string
	LogPath   string
	HTTPS     bool
	SSLPath   string
	ABTest    bool
}

type UpstreamConf struct {
	NginxPath    string
	ConsulAddr   string
	ConsulPrefix string
}

func replace(input, from, to string) string {
	return strings.Replace(input, from, to, -1)
}

func Init(conf InitConf) error {
	var err error

	nginxConfTmpl, err = template.ParseFiles(conf.NginxPath + "tmpl/nginx.conf.tmpl")
	if err != nil {
		return err
	}

	proxyConfTmpl, err = template.ParseFiles(conf.NginxPath + "tmpl/proxy.conf.tmpl")
	if err != nil {
		return err
	}

	upstreamTmpl, err = template.ParseFiles(conf.NginxPath + "tmpl/upstream.conf.tmpl")
	if err != nil {
		return err
	}

	serverTmpl, err = template.ParseFiles(conf.NginxPath + "tmpl/server.conf.tmpl")
	if err != nil {
		return err
	}

	nginxConf := NginxConf{
		NginxPath:                 conf.NginxPath,
		LogPath:                   conf.LogPath,
		ServerName:                conf.ServerName,
		PidPath:                   conf.PidPath,
		ServerNamesHashMaxSize:    conf.ServerNamesHashMaxSize,
		ServerNamesHashBucketSize: conf.ServerNamesHashBucketSize,
		CheckShmSize:              conf.CheckShmSize,
		ABTest:                    conf.ABTest,
		RedisConf:                 conf.RedisConf,
	}

	if err := renderNginxConf(nginxConf); err != nil {
		return err
	}

	log.Debugln("render nginx.conf success")

	proxyConf := ProxyConf{
		NginxPath: conf.NginxPath,
		ABTest:    conf.ABTest,
		RedisConf: conf.RedisConf,
	}

	if err := renderProxyConf(proxyConf); err != nil {
		return err
	}

	log.Debugln("render proxy.conf success")

	if f, err := os.Create(conf.NginxPath + "conf/server.conf"); err != nil {
		return err
	} else {
		err = f.Close()
		if err != nil {
			return err
		}
	}

	log.Debugln("create server.conf success")

	if f, err := os.Create(conf.NginxPath + "conf/upstream.conf"); err != nil {
		return err
	} else {
		err = f.Close()
		if err != nil {
			return err
		}
	}

	log.Debugln("create upstream.conf success")

	_, err = os.Stat(conf.NginxPath + "upstreams")
	if os.IsNotExist(err) {
		if err := os.Mkdir(conf.NginxPath+"upstreams", os.ModePerm); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	log.Debugln("mkdir " + conf.NginxPath + "upstreams success")

	_, err = os.Stat(conf.LogPath)
	if os.IsNotExist(err) {
		if err := os.Mkdir(conf.LogPath, os.ModePerm); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	log.Debugln("mkdir " + conf.LogPath + " success")

	if conf.HTTPS {
		if err := loadCrt(conf.SSLPath); err != nil {
			return err
		}
	}

	if f, err := os.Create(conf.NginxPath + "lock"); err != nil {
		return err
	} else {
		err = f.Close()
		if err != nil {
			return err
		}
	}

	log.Debugln("create lock success")

	return nil
}

func renderNginxConf(conf NginxConf) error {
	f, err := os.Create(conf.NginxPath + "conf/nginx.conf")
	if err != nil {
		return err
	}
	err = nginxConfTmpl.Execute(f, conf)
	if err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func renderProxyConf(conf ProxyConf) error {
	f, err := os.Create(conf.NginxPath + "conf/proxy.conf")
	if err != nil {
		return err
	}
	err = proxyConfTmpl.Execute(f, conf)
	if err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func renderServerConf(config *Config, conf ServerConf) error {
	f, err := os.Create(conf.NginxPath + "conf/server.conf")
	if err != nil {
		return err
	}
	err = serverTmpl.Execute(f, map[string]interface{}{
		"Conf":    conf,
		"Servers": config.Servers,
		"Replace": replace,
	})
	if err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func renderUpstreamConf(config *Config, conf UpstreamConf) error {
	f, err := os.Create(conf.NginxPath + "conf/upstream.conf")
	if err != nil {
		return err
	}
	err = upstreamTmpl.Execute(f, map[string]interface{}{
		"ConsulAddr":   conf.ConsulAddr,
		"ConsulPrefix": conf.ConsulPrefix,
		"Upstreams":    config.Upstreams,
	})
	if err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func loadCrt(sslPath string) error {
	certs = make(map[string]*x509.Certificate)
	files, err := ioutil.ReadDir(sslPath)
	if err != nil {
		return err
	}
	for _, file := range files {
		if strings.Contains(file.Name(), ".crt") {
			bytes, err := ioutil.ReadFile(sslPath + file.Name())
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

func fixSSL(config *Config) {
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

func fixABTest(config *Config) {
	for serverName, server := range config.Servers {
		for uri, location := range server.Locations {
			if _, ok := config.Upstreams[location.Upstream+"_canary"]; ok {
				v := config.Servers[serverName].Locations[uri]
				v.ABTest = true
				config.Servers[serverName].Locations[uri] = v
				log.WithFields(log.Fields{
					"server":   serverName,
					"location": uri,
				}).Debugln("ABTest=true")
			}
		}
	}
}

func Reload(path string) error {
	pidfile.SetPidfilePath(path)
	pid, err := pidfile.Read()
	if err != nil {
		return err
	}
	err = syscall.Kill(pid, syscall.SIGHUP)
	if err != nil {
		return err
	}
	log.WithFields(log.Fields{
		"pidPath": path,
		"pid":     pid,
	}).Debugln("nginx reload success")
	return nil
}

func Render(config *Config, conf RenderConf) error {
	if conf.HTTPS {
		fixSSL(config)
	}
	if conf.ABTest {
		fixABTest(config)
	}
	serverConf := ServerConf{
		NginxPath: conf.NginxPath,
		LogPath:   conf.LogPath,
		HTTPS:     conf.HTTPS,
		SSLPath:   conf.SSLPath,
		ABTest:    conf.ABTest,
	}
	if err := renderServerConf(config, serverConf); err != nil {
		return err
	}
	upstreamConf := UpstreamConf{
		NginxPath:    conf.NginxPath,
		ConsulAddr:   conf.ConsulAddr,
		ConsulPrefix: conf.ConsulPrefix,
	}
	if err := renderUpstreamConf(config, upstreamConf); err != nil {
		return err
	}
	return nil
}
