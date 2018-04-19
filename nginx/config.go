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

type Location struct {
	Upstream  string
	HttpsOnly bool
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

func Init(nginxPath string, logPath string, serverName string, pidPath string, https bool, sslPath string, serverNamesHashMaxSize int, serverNamesHashBucketSize int) error {
	var err error

	nginxConfTmpl, err = template.ParseFiles(nginxPath + "tmpl/nginx.conf.tmpl")
	if err != nil {
		return err
	}

	upstreamTmpl, err = template.ParseFiles(nginxPath + "tmpl/upstream.conf.tmpl")
	if err != nil {
		return err
	}

	serverTmpl, err = template.ParseFiles(nginxPath + "tmpl/server.conf.tmpl")
	if err != nil {
		return err
	}

	if err := renderNginxConf(nginxPath, logPath, pidPath, serverName, serverNamesHashMaxSize, serverNamesHashBucketSize); err != nil {
		return err
	}

	log.WithFields(log.Fields{
		"nginxPath":                 nginxPath,
		"logPath":                   logPath,
		"pidPath":                   pidPath,
		"serverName":                serverName,
		"serverNamesHashMaxSize":    serverNamesHashMaxSize,
		"serverNamesHashBucketSize": serverNamesHashBucketSize,
	}).Debugln("render nginx.conf success")

	if f, err := os.Create(nginxPath + "conf/server.conf"); err != nil {
		return err
	} else {
		err = f.Close()
		if err != nil {
			return err
		}
	}

	log.Debugln("create server.conf success")

	if f, err := os.Create(nginxPath + "conf/upstream.conf"); err != nil {
		return err
	} else {
		err = f.Close()
		if err != nil {
			return err
		}
	}

	log.Debugln("create upstream.conf success")

	_, err = os.Stat(nginxPath + "upstreams")
	if os.IsNotExist(err) {
		if err := os.Mkdir(nginxPath+"upstreams", os.ModePerm); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	log.Debugln("mkdir " + nginxPath + "upstreams success")

	_, err = os.Stat(logPath)
	if os.IsNotExist(err) {
		if err := os.Mkdir(logPath, os.ModePerm); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	log.Debugln("mkdir " + logPath + " success")

	if https {
		if err := loadCrt(sslPath); err != nil {
			return err
		}
	}

	if f, err := os.Create(nginxPath + "lock"); err != nil {
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

func renderNginxConf(nginxPath string, logPath string, pidPath string, serverName string, serverNamesHashMaxSize int, serverNamesHashBucketSize int) error {
	f, err := os.Create(nginxPath + "conf/nginx.conf")
	if err != nil {
		return err
	}
	err = nginxConfTmpl.Execute(f, map[string]interface{}{
		"NginxPath":                 nginxPath,
		"LogPath":                   logPath,
		"PidPath":                   pidPath,
		"ServerName":                serverName,
		"ServerNamesHashMaxSize":    serverNamesHashMaxSize,
		"ServerNamesHashBucketSize": serverNamesHashBucketSize,
	})
	if err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func renderServerConf(config *Config, nginxPath string, logPath string, https bool, sslPath string) error {
	f, err := os.Create(nginxPath + "conf/server.conf")
	if err != nil {
		return err
	}
	err = serverTmpl.Execute(f, map[string]interface{}{
		"HTTPS":     https,
		"NginxPath": nginxPath,
		"LogPath":   logPath,
		"SSLPath":   sslPath,
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

func render(config *Config, consulAddr string, consulPrefix string, nginxPath string, logPath string, https bool, sslPath string) error {
	if err := renderServerConf(config, nginxPath, logPath, https, sslPath); err != nil {
		return err
	}
	if err := renderUpstreamConf(config, consulAddr, consulPrefix, nginxPath); err != nil {
		return err
	}
	return nil
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

func Render(config *Config, consulAddr string, consulPrefix, nginxPath string, logPath string, https bool, sslPath string) error {
	if https {
		matchSSL(config)
	}
	return render(config, consulAddr, consulPrefix, nginxPath, logPath, https, sslPath)
}
