package lainlet

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"github.com/laincloud/webrouter/nginx"
	log "github.com/sirupsen/logrus"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
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

func WatchConfig(addr string) <-chan nginx.Config {
	respCh := make(chan nginx.Config)
	go func() {
		defer close(respCh)
		for {
		START1:
			resp, err := http.Get("http://" + addr + "/v2/webrouter/webprocs?watch=1")
			if err != nil {
				respCh <- nginx.Config{
					Err: err,
				}
				continue
			}
			reader := bufio.NewReader(resp.Body)
			for {
			START2:
				data := new(WebrouterInfo)
				line, err := reader.ReadBytes('\n')
				if err != nil {
					respCh <- nginx.Config{
						Err: err,
					}
					if err == io.EOF {
						time.Sleep(time.Second)
						goto START1
					} else {
						continue
					}
				}
				fields := bytes.SplitN(bytes.TrimSpace(line), []byte{':'}, 2)
				if len(fields) < 2 {
					continue
				}
				key := string(bytes.TrimSpace(fields[0]))
				if key == "data" {
					err := json.Unmarshal(bytes.TrimSpace(fields[1]), &data.Data)
					if err != nil {
						log.Errorln(err)
						continue
					}
					var config nginx.Config
					config.Servers = make(map[string]nginx.Server)
					config.Upstreams = make(map[string]nginx.Upstream)
					for k, v := range data.Data {
						s := strings.Split(k, ".")
						if len(s) != 3 {
							continue
						}
						if len(v.PodInfos) < 1 {
							continue
						}
						name := strings.Replace(k, ".", "_", -1)
						annotation := new(Annotation)
						json.Unmarshal([]byte(v.PodInfos[0].Annotation), annotation)
						if !strings.HasSuffix(s[2], "_canary") {
							for _, mountPoint := range annotation.MountPoint {
								var serverName, uri string
								if mountPoint[len(mountPoint)-1] == '/' {
									mountPoint = mountPoint[0 : len(mountPoint)-1]
								}
								if strings.Index(mountPoint, "/") > 0 {
									serverName = mountPoint[0:strings.Index(mountPoint, "/")]
									uri = mountPoint[strings.Index(mountPoint, "/")+1:]
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
										respCh <- nginx.Config{
											Err: errors.New("servername: " + serverName + " location: " + uri +
												" upstream1: " + config.Servers[serverName].Locations[uri].Upstream +
												" upstream2: " + name + " duplicate location !"),
										}
										goto START2
									}
								}

								config.Servers[serverName].Locations[uri] = nginx.Location{
									Upstream:  name,
									HttpsOnly: annotation.HttpsOnly,
								}
							}
						}
						var servers []string
						for _, container := range v.PodInfos {
							if container.Containers[0].IP != "" {
								addr := container.Containers[0].IP + ":" + strconv.Itoa(container.Containers[0].Expose)
								_, err := net.ResolveTCPAddr("tcp4", addr)
								if err != nil {
									log.Errorln(err)
									continue
								}
								servers = append(servers, addr)
							}
						}
						if len(servers) == 0 {
							log.Errorln("no servers are inside upstream " + name)
							servers = append(servers, "127.0.0.1:11111")
						}
						config.Upstreams[name] = nginx.Upstream{
							HealthCheck: annotation.HealthCheck,
							Servers:     servers,
						}
					}
					config.Err = nil
					respCh <- config
				}
			}
		}
	}()
	return respCh
}
