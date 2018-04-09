package main

import (
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-cleanhttp"
	"github.com/laincloud/webrouter/graphite"
	"github.com/laincloud/webrouter/lainlet"
	"github.com/onrik/logrus/filename"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"time"
)

func main() {

	log.AddHook(filename.NewHook())

	viper.SetDefault("lainlet", "lainlet.lain:9001")
	viper.SetDefault("consul", "consul.lain:8500")
	viper.SetDefault("prefix", "lain/webrouter/upstreams/")
	viper.SetDefault("graphite", false)
	viper.SetDefault("graphiteHost", nil)
	viper.SetDefault("graphitePort", nil)

	viper.BindEnv("lainlet", "LAINLET_ADDR")
	viper.BindEnv("consul", "CONSUL_ADDR")
	viper.BindEnv("prefix", "CONSUL_KEY_PREFIX")
	viper.BindEnv("graphite", "GRAPHITE_ENABLE")
	viper.BindEnv("graphiteHost", "GRAPHITE_HOST")
	viper.BindEnv("graphitePort", "GRAPHITE_PORT")

	lainletAddr := viper.GetString("lainlet")
	consulAddr := viper.GetString("consul")
	prefix := viper.GetString("prefix")
	graphiteEnable := viper.GetBool("graphite")
	var graphiteHost string
	var graphitePort int
	if graphiteEnable {
		graphiteHost = viper.GetString("graphiteHost")
		graphitePort = viper.GetInt("graphitePort")
	}

	config := &api.Config{
		Address:   consulAddr,
		Scheme:    "http",
		Transport: cleanhttp.DefaultPooledTransport(),
	}

	client, err := api.NewClient(config)
	if err != nil {
		log.Fatalln(err)
	}

	health := 1

	ticker := time.NewTicker(1 * time.Minute)
	if graphiteEnable {
		go func() {
			for range ticker.C {
				graphite.SendConfdMetrics(graphiteHost, graphitePort, health)
			}
		}()
	}

	for {
		watchCh := lainlet.WatchUpstream(lainletAddr)
		for {
			upstreams, ok := <-watchCh
			if ok {
				for k, newServers := range upstreams {
					key := prefix + k
					var servers []string
					for {
						servers, _, err = client.KV().Keys(key, "", &api.QueryOptions{RequireConsistent: true})
						if err != nil {
							health = 0
							log.Errorln(err)
						} else {
							break
						}
					}

					for i, server := range servers {
						servers[i] = server[len(key)+1:]
					}
					deleted, added := diff(servers, newServers)
					for _, server := range added {
						p := &api.KVPair{Key: key + "/" + server, Value: []byte("")}
						for {
							_, err := client.KV().Put(p, nil)
							if err != nil {
								health = 0
								log.Errorln(err)
							} else {
								break
							}
						}

					}

					for _, server := range deleted {
						for {
							_, err := client.KV().Delete(key+"/"+server, nil)
							if err != nil {
								health = 0
								log.Errorln(err)
							} else {
								break
							}
						}

					}
					health = 1
				}
			}
		}
	}
}

func diff(slice1, slice2 []string) ([]string, []string) {
	var deleted, added []string
	m := map[string]int{}

	for _, s := range slice1 {
		m[s] = 1
	}
	for _, s := range slice2 {
		if m[s] == 1 {
			m[s] = 2
		}
	}

	for mKey, mVal := range m {
		if mVal == 1 {
			deleted = append(deleted, mKey)
		}
	}

	m = map[string]int{}

	for _, s := range slice2 {
		m[s] = 1
	}

	for _, s := range slice1 {
		if m[s] == 1 {
			m[s] = 2
		}
	}

	for mKey, mVal := range m {
		if mVal == 1 {
			added = append(added, mKey)
		}
	}

	return deleted, added
}
