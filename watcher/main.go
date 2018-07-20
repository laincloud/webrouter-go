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
	viper.SetDefault("checkShmSize",1)
	viper.SetDefault("debug", false)
	viper.SetDefault("graphite", false)
	viper.SetDefault("ABTest", false)
	viper.SetDefault("redisRole", "master")
	viper.SetDefault("redisDBID", 0)
	viper.SetDefault("redisConnectTimeout", 100)
	viper.SetDefault("redisReadTimeout", 1000)
	viper.SetDefault("redisKeepaliveTimeout", 60000)
	viper.SetDefault("redisPoolSize", 30)

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
	viper.BindEnv("checkShmSize","CHECK_SHM_SIZE")
	viper.BindEnv("debug", "DEBUG")
	viper.BindEnv("graphite", "GRAPHITE_ENABLE")
	viper.BindEnv("graphiteHost", "GRAPHITE_HOST")
	viper.BindEnv("graphitePort", "GRAPHITE_PORT")
	viper.BindEnv("ABTest", "AB_TEST")
	viper.BindEnv("redisSentinel", "REDIS_SENTINEL")
	viper.BindEnv("redisMasterName", "REDIS_MASTER_NAME")
	viper.BindEnv("redisRole", "REDIS_ROLE")
	viper.BindEnv("redisPassword", "REDIS_PASSWORD")
	viper.BindEnv("redisConnectTimeout", "REDIS_CONNECT_TIMEOUT")
	viper.BindEnv("redisReadTimeout", "REDIS_READ_TIMEOUT")
	viper.BindEnv("redisDBID", "REDIS_DBID")
	viper.BindEnv("redisPoolSize", "REDIS_POOL_SIZE")
	viper.BindEnv("redisKeepaliveTimeout", "REDIS_KEEPALIVE_TIMEOUT")

	lainletAddr := viper.GetString("lainlet")
	pidPath := viper.GetString("pid")
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

	redisConf := nginx.RedisConf{
		Sentinel:         viper.GetString("redisSentinel"),
		MasterName:       viper.GetString("redisMasterName"),
		Role:             viper.GetString("redisRole"),
		Password:         viper.GetString("redisPassword"),
		ConnectTimeout:   viper.GetInt("redisConnectTimeout"),
		ReadTimeout:      viper.GetInt("redisReadTimeout"),
		DBID:             viper.GetInt("redisDBID"),
		PoolSize:         viper.GetInt("redisPoolSize"),
		KeepaliveTimeout: viper.GetInt("redisKeepaliveTimeout"),
	}

	initConf := nginx.InitConf{
		NginxPath:                 viper.GetString("nginx"),
		LogPath:                   viper.GetString("log"),
		ServerName:                viper.GetString("serverName"),
		PidPath:                   viper.GetString("pid"),
		HTTPS:                     viper.GetBool("https"),
		SSLPath:                   viper.GetString("ssl"),
		ServerNamesHashMaxSize:    viper.GetInt("serverNamesHashMaxSize"),
		ServerNamesHashBucketSize: viper.GetInt("serverNamesHashBucketSize"),
		CheckShmSize:              viper.GetInt("checkShmSize"),
		ABTest:    viper.GetBool("ABTest"),
		RedisConf: redisConf,
	}

	randerConf := nginx.RenderConf{
		NginxPath:    viper.GetString("nginx"),
		LogPath:      viper.GetString("log"),
		HTTPS:        viper.GetBool("https"),
		SSLPath:      viper.GetString("ssl"),
		ConsulAddr:   viper.GetString("consul"),
		ConsulPrefix: viper.GetString("prefix"),
		ABTest:       viper.GetBool("ABTest"),
		RedisConf:    redisConf,
	}

	err := nginx.Init(initConf)
	if err != nil {
		log.Fatalln(err)
	}

	for {
		if _, err := os.Stat(pidPath); err != nil {
			log.Errorln(err)
			time.Sleep(time.Second)
		} else {
			break
		}
	}

	health := 0

	if graphiteEnable {
		ticker := time.NewTicker(1 * time.Minute)
		go func() {
			for range ticker.C {
				graphite.SendOpenRestyMetrics(graphiteHost, graphitePort, health)
			}
		}()
	}

	var servers interface{}
	watchCh := lainlet.WatchConfig(lainletAddr)
	for {
		newConfig, ok := <-watchCh
		if ok {
			if newConfig.Err != nil {
				health = 0
				log.Errorln(newConfig.Err)
				continue
			}
			newServers, err := copystructure.Copy(newConfig.Servers)
			if err != nil {
				health = 0
				log.Errorln(err)
				continue
			}
			if err := nginx.Render(&newConfig, randerConf); err != nil {
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
			if !reflect.DeepEqual(servers, newServers) {
				if err := nginx.Reload(pidPath); err != nil {
					health = 0
					log.Errorln(err)
					continue
				}
				servers = newServers
			}
			health = 1
		}
	}
}
