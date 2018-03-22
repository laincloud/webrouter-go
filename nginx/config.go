package nginx

import (
	"crypto/x509"
	"encoding/pem"
	"github.com/facebookgo/pidfile"
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

type Config struct {
	Servers   map[string]Server
	Upstreams map[string]string
}

func Init(nginxPath string, logPath string, serverName string, pidPath string) error {
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

	if err := renderNginxConf(nginxPath, logPath, pidPath, serverName); err != nil {
		return err
	}

	if err := loadCrt(nginxPath); err != nil {
		return err
	}

	if f, err := os.Create(nginxPath + "conf/server.conf"); err != nil {
		return err
	} else {
		err = f.Close()
		if err != nil {
			return err
		}
	}

	if f, err := os.Create(nginxPath + "conf/upstream.conf"); err != nil {
		return err
	} else {
		err = f.Close()
		if err != nil {
			return err
		}
	}

	_, err = os.Stat(nginxPath + "upstreams")
	if os.IsNotExist(err) {
		if err := os.Mkdir(nginxPath+"upstreams", os.ModePerm); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	if f, err := os.Create(nginxPath + "lock"); err != nil {
		return err
	} else {
		err = f.Close()
		if err != nil {
			return err
		}
	}

	return nil
}

func renderNginxConf(nginxPath string, logPath string, pidPath string, serverName string) error {
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
	certs = make(map[string]*x509.Certificate)
	files, err := ioutil.ReadDir(nginxPath + "ssl")
	if err != nil {
		return err
	}
	for _, file := range files {
		if strings.Contains(file.Name(), ".crt") {
			bytes, err := ioutil.ReadFile(nginxPath + "ssl/" + file.Name())
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
