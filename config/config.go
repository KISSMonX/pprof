package config

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
)

// sourceConf 各服务 pprof 接口配置
type sourceConf struct {
	Host string `json:"host"`
	Port string `json:"port"`

	Sources []struct {
		Name    string `json:"name"`
		Host    string `json:"host"`
		Port    string `json:"port"`
		IsInner bool   `json:"is_inner"`
		Comment string `json:"comment"`
	} `json:"sources"`
}

var (
	// Config 各服务 pprof 接口配置
	Config sourceConf
)

// LoadConfig 读取各服务路径 url 配置信息
func LoadConfig() error {
	f, err := ioutil.ReadFile("sources.conf")
	if err != nil {
		log.Panicln("读取配置文件失败: ", err)
		return err
	}
	err = json.Unmarshal(f, &Config)
	if err != nil {
		log.Panicln("解析配置文件失败: ", err)
		return err
	}

	for _, item := range Config.Sources {
		log.Printf("%s: %s:%s  isInner: %v Comment: %s", item.Name, item.Host, item.Port, item.IsInner, item.Comment)
	}

	return nil
}

// GetServiceSource 获取指定服务名称的 source 路径
func GetServiceSource(serviceName string) (source string, err error) {
	if len(serviceName) == 0 {
		log.Println("必须指定服务名称: ", serviceName)
		return "", errors.New("没有指定服务名称")
	}

	for k, v := range Config.Sources {
		log.Println(k, v)
		if v.Name == serviceName {
			source = v.Host + ":" + v.Port
			break
		}
	}

	return source, err
}

// GetHTTPServeHostPort 获取本 pprof 服务的 IP 和端口
func GetHTTPServeHostPort() string {
	return Config.Host + ":" + Config.Port
}
