package main

import (
	"bytes"
	"github.com/laincloud/webrouter/graphite"
	"github.com/laincloud/webrouter/lainlet"
	"github.com/laincloud/webrouter/nginx"
	"github.com/mitchellh/copystructure"
	"github.com/onrik/logrus/filename"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"os"
	"os/exec"
	"reflect"
	"time"
)

func main() {

	log.AddHook(filename.NewHook())

	viper.SetDefault("lainlet", "lainlet.lain:9001")
	viper.SetDefault("consul", "consul.lain:8500")
	viper.SetDefault("nginx", "/usr/local/openresty/nginx/")
	viper.SetDefault("pid", "/var/run/nginx.pid")
	viper.SetDefault("log", "/var/log/nginx/")
	viper.SetDefault("ssl", "/etc/nginx/ssl/")
	viper.SetDefault("serverName", "localhost")
	viper.SetDefault("prefix", "lain/webrouter/upstreams/")
	viper.SetDefault("https", false)
	viper.SetDefault("serverNamesHashMaxSize", 512)
	viper.SetDefault("serverNamesHashBucketSize", 64)
	viper.SetDefault("debug", false)
	viper.SetDefault("graphite", false)
	viper.SetDefault("graphiteHost", nil)
	viper.SetDefault("graphitePort", nil)

	viper.BindEnv("lainlet", "LAINLET_ADDR")
	viper.BindEnv("consul", "CONSUL_ADDR")
	viper.BindEnv("nginx", "NGINX_PATH")
	viper.BindEnv("pid", "NGINX_PID_PATH")
	viper.BindEnv("log", "NGINX_LOG_PATH")
	viper.BindEnv("ssl", "NGINX_SSL_PATH")
	viper.BindEnv("serverName", "NGINX_SERVER_NAME")
	viper.BindEnv("prefix", "CONSUL_KEY_PREFIX")
	viper.BindEnv("https", "HTTPS")
	viper.BindEnv("serverNamesHashMaxSize", "SERVER_NAMES_HASH_MAX_SIZE")
	viper.BindEnv("serverNamesHashBucketSize", "SERVER_NAMES_HASH_BUCKET_SIZE")
	viper.BindEnv("debug", "DEBUG")
	viper.BindEnv("graphite", "GRAPHITE_ENABLE")
	viper.BindEnv("graphiteHost", "GRAPHITE_HOST")
	viper.BindEnv("graphitePort", "GRAPHITE_PORT")

	lainletAddr := viper.GetString("lainlet")
	consulAddr := viper.GetString("consul")
	nginxPath := viper.GetString("nginx")
	pidPath := viper.GetString("pid")
	logPath := viper.GetString("log")
	sslPath := viper.GetString("ssl")
	serverName := viper.GetString("serverName")
	consulPrefix := viper.GetString("prefix")
	https := viper.GetBool("https")
	serverNamesHashMaxSize := viper.GetInt("serverNamesHashMaxSize")
	serverNamesHashBucketSize := viper.GetInt("serverNamesHashBucketSize")
	debug := viper.GetBool("debug")
	graphiteEnable := viper.GetBool("graphite")
	var graphiteHost string
	var graphitePort int
	if graphiteEnable {
		graphiteHost = viper.GetString("graphiteHost")
		graphitePort = viper.GetInt("graphitePort")
	}
	if debug {
		log.SetLevel(log.DebugLevel)
	}

	err := nginx.Init(nginxPath, logPath, serverName, pidPath, https, sslPath, serverNamesHashMaxSize, serverNamesHashBucketSize)
	if err != nil {
		log.Fatalln(err)
	}

	health := 1

	ticker := time.NewTicker(1 * time.Minute)
	if graphiteEnable {
		go func() {
			for range ticker.C {
				graphite.SendOpenRestyMetrics(graphiteHost, graphitePort, health)
			}
		}()
	}

	var servers interface{}

	for {
		if _, err := os.Stat(pidPath); err != nil {
			health = 0
			log.Errorln(err)
			time.Sleep(time.Second)
			continue
		}
		watchCh := lainlet.WatchConfig(lainletAddr)
		for {
			newConfig, ok := <-watchCh
			if ok {
				if newConfig.Err != nil {
					health = 0
					log.Errorln(newConfig.Err)
					continue
				}
				if !reflect.DeepEqual(servers, newConfig.Servers) {
					newServers, err := copystructure.Copy(newConfig.Servers)
					if err != nil {
						health = 0
						log.Errorln(err)
						continue
					}
					if err := nginx.Render(&newConfig, consulAddr, consulPrefix, nginxPath, logPath, https, sslPath); err != nil {
						health = 0
						log.Errorln(err)
						continue
					}
					cmd := exec.Command("nginx", "-t")
					var stderr bytes.Buffer
					cmd.Stderr = &stderr
					if err != nil {
						health = 0
						log.Errorln(err)
						log.Errorln(string(stderr.Bytes()))
						continue
					}
					if err := nginx.Reload(pidPath); err != nil {
						health = 0
						log.Errorln(err)
						continue
					}
					servers = newServers
					health = 1
				} else {
					health = 1
				}
			}
		}
	}
}
