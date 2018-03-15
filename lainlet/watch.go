package lainlet

import (
	"bufio"
	"bytes"
	"encoding/json"
	"github.com/laincloud/webrouter/nginx"
	log "github.com/sirupsen/logrus"
	"net/http"
	"strconv"
	"strings"
)

type ContainerForWebrouter struct {
	IP     string `json:"ContainerIp"`
	Expose int
}

type PodInfoForWebrouter struct {
	Annotation string
	Containers []ContainerForWebrouter `json:"ContainerInfos"`
}

type CoreInfoForWebrouter struct {
	PodInfos []PodInfoForWebrouter
}

type WebrouterInfo struct {
	Data map[string]CoreInfoForWebrouter
}

type Annotation struct {
	MountPoint  []string `json:"mountpoint"`
	HttpsOnly   bool     `json:"https_only"`
	HealthCheck string   `json:"healthcheck"`
}

func WatchConfig(addr string) (<-chan *nginx.Config, error) {
	resp, err := http.Get("http://" + addr + "/v2/webrouter/webprocs?watch=1")
	if err != nil {
		return nil, err
	}
	reader := bufio.NewReader(resp.Body)
	respCh := make(chan *nginx.Config)
	go func() {
		defer close(respCh)
		for {
		START:
			data := new(WebrouterInfo)
			line, err := reader.ReadBytes('\n')
			if err != nil {
				log.Fatalln(err)
			}
			fields := bytes.SplitN(bytes.TrimSpace(line), []byte{':'}, 2)
			if len(fields) < 2 {
				continue
			}
			key := string(bytes.TrimSpace(fields[0]))
			if key == "data" {
				err := json.Unmarshal(bytes.TrimSpace(fields[1]), &data.Data)
				if err != nil {
					continue
				}
				config := new(nginx.Config)
				config.Servers = make(map[string]nginx.Server)
				config.Upstreams = make(map[string]string)
				for k, v := range data.Data {
					if len(v.PodInfos) < 1 {
						continue
					}
					name := strings.Replace(k, ".", "_", -1)
					annotation := new(Annotation)
					json.Unmarshal([]byte(v.PodInfos[0].Annotation), annotation)
					if len(annotation.MountPoint) < 1 {
						continue
					}
					for _, mountPoint := range annotation.MountPoint {
						var serverName, uri string
						if strings.Index(mountPoint, "/") > 0 {
							serverName = mountPoint[0 : strings.Index(mountPoint, "/")-1]
							uri = mountPoint[strings.Index(mountPoint, "/")-1:]
						} else {
							serverName = mountPoint
							uri = "/"
						}
						if _, ok := config.Servers[serverName]; !ok {
							config.Servers[serverName] = nginx.Server{
								Locations: make(map[string]nginx.Location),
							}
						} else {
							if config.Servers[serverName].Locations[uri].Upstream != "" {
								log.WithFields(log.Fields{
									"servername": serverName,
									"location":   uri,
									"upstream1":  config.Servers[serverName].Locations[uri].Upstream,
									"upstream2":  name,
								}).Errorln("duplicate location !")
								goto START
							}
						}

						config.Servers[serverName].Locations[uri] = nginx.Location{
							Upstream:  name,
							HttpsOnly: annotation.HttpsOnly,
						}
					}
					config.Upstreams[name] = annotation.HealthCheck
				}
				respCh <- config
			}
		}
	}()
	return respCh, nil
}

func WatchUpstream(addr string) (<-chan map[string][]string, error) {
	resp, err := http.Get("http://" + addr + "/v2/webrouter/webprocs?watch=1")
	if err != nil {
		return nil, err
	}
	reader := bufio.NewReader(resp.Body)
	respCh := make(chan map[string][]string)
	go func() {
		defer close(respCh)
		for {
			data := new(WebrouterInfo)
			line, err := reader.ReadBytes('\n')
			if err != nil {
				log.Fatalln(err)
			}
			fields := bytes.SplitN(bytes.TrimSpace(line), []byte{':'}, 2)
			if len(fields) < 2 {
				continue
			}
			key := string(bytes.TrimSpace(fields[0]))
			if key == "data" {
				err := json.Unmarshal(bytes.TrimSpace(fields[1]), &data.Data)
				if err != nil {
					continue
				}
				upstreams := make(map[string][]string)
				for k, v := range data.Data {
					if len(v.PodInfos) < 1 {
						continue
					}
					name := strings.Replace(k, ".", "_", -1)
					if len(v.PodInfos) < 1 {
						continue
					}
					for _, container := range v.PodInfos {
						upstreams[name] = append(upstreams[name], container.Containers[0].IP+":"+strconv.Itoa(container.Containers[0].Expose))
					}
				}
				respCh <- upstreams
			}
		}
	}()
	return respCh, nil
}
