package graphite

import (
	"github.com/marpaia/graphite-golang"
	"os"
	"strconv"
	"strings"
)

func send(host string, port int, key string, value string) error {
	gh, err := graphite.NewGraphite(host, port)
	if err != nil {
		return err
	}
	if err := gh.SimpleSend(key, value); err != nil {
		return err
	}
	return nil
}

func SendOpenRestyMetrics(host string, port int, health int) error {
	key := strings.Replace(os.Getenv("LAIN_DOMAIN"), ".", "_", -1) + ".webrouter.openresty." + os.Getenv("DEPLOYD_POD_INSTANCE_NO") + ".health"
	return send(host, port, key, strconv.Itoa(health))
}

func SendConfdMetrics(host string, port int, health int) error {
	key := strings.Replace(os.Getenv("LAIN_DOMAIN"), ".", "_", -1) + ".webrouter.confd.health"
	return send(host, port, key, strconv.Itoa(health))
}
