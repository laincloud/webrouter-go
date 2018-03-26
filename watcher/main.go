package main

import (
	"github.com/laincloud/webrouter/lainlet"
	"github.com/laincloud/webrouter/nginx"
	"github.com/onrik/logrus/filename"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"os"
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
	viper.SetDefault("serverNamesHashMaxSize","512")

	viper.BindEnv("lainlet", "LAINLET_ADDR")
	viper.BindEnv("consul", "CONSUL_ADDR")
	viper.BindEnv("nginx", "NGINX_PATH")
	viper.BindEnv("pid", "NGINX_PID_PATH")
	viper.BindEnv("log", "NGINX_LOG_PATH")
	viper.BindEnv("ssl", "NGINX_SSL_PATH")
	viper.BindEnv("serverName", "NGINX_SERVER_NAME")
	viper.BindEnv("prefix", "CONSUL_KEY_PREFIX")
	viper.BindEnv("https", "HTTPS")
	viper.BindEnv("serverNamesHashMaxSize","SERVER_NAMES_HASH_MAX_SIZE")

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

	err := nginx.Init(nginxPath, logPath, serverName, pidPath, https, sslPath,serverNamesHashMaxSize)
	if err != nil {
		log.Fatalln(err)
	}

	var config *nginx.Config

	for {
		if _, err := os.Stat(pidPath); err != nil {
			log.Errorln(err)
			time.Sleep(time.Second)
			continue
		}
		watchCh := lainlet.WatchConfig(lainletAddr)
		for {
			newConfig, ok := <-watchCh
			if ok {
				if !reflect.DeepEqual(config, newConfig) {
					config = newConfig
					err := nginx.Reload(config, consulAddr, consulPrefix, nginxPath, pidPath, logPath, https, sslPath)
					if err != nil {
						log.Errorln(err)
						continue
					}
				}
			}
		}
	}
}
