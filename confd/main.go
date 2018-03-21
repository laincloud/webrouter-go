package main

import (
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-cleanhttp"
	"github.com/laincloud/webrouter/lainlet"
	"github.com/onrik/logrus/filename"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func main() {

	log.AddHook(filename.NewHook())

	viper.SetDefault("lainlet", "lainlet.lain:9001")
	viper.SetDefault("consul", "consul.lain:8500")
	viper.SetDefault("prefix", "lain/webrouter/upstreams/")

	viper.BindEnv("lainlet", "LAINLET_ADDR")
	viper.BindEnv("consul", "CONSUL_ADDR")
	viper.BindEnv("prefix", "CONSUL_KEY_PREFIX")

	lainletAddr := viper.GetString("lainlet")
	consulAddr := viper.GetString("consul")
	prefix := viper.GetString("prefix")
	config := &api.Config{
		Address:   consulAddr,
		Scheme:    "http",
		Transport: cleanhttp.DefaultPooledTransport(),
	}

	client, err := api.NewClient(config)
	if err != nil {
		log.Fatalln(err)
	}

	for {
		watchCh, err := lainlet.WatchUpstream(lainletAddr)
		if err != nil {
			log.Errorln(err)
			continue
		}
		for {
			upstreams, ok := <-watchCh
			if ok {
				for k, newServers := range upstreams {
					key := prefix + k
					var servers []string
					for {
						servers, _, err = client.KV().Keys(key, "", &api.QueryOptions{RequireConsistent: true})
						if err != nil {
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
								log.Errorln(err)
							} else {
								break
							}
						}

					}
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
