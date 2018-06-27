package config

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"path/filepath"
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
	absPath, _ := filepath.Abs("sources.cfg")
	f, err := ioutil.ReadFile(absPath)
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
		log.Printf("%-16s: %-22s 内网接口: %-5v 备注: %s", item.Name, item.Host+":"+item.Port, item.IsInner, item.Comment)
	}

	return nil
}

// GetServiceSource 获取指定服务名称的 source 路径
func GetServiceSource(serviceName string) (source string, err error) {
	if len(serviceName) == 0 {
		log.Println("必须指定服务名称: ", serviceName)
		return "", errors.New("没有指定服务名称")
	}

	var host, port string
	for k, v := range Config.Sources {
		log.Println(k, v)
		if v.Name == serviceName {
			host = v.Host
			port = v.Port
			break
		}
	}

	if host == "" || port == "" {
		log.Println("服务地址或端口不存在, 可能没有注册: ", host, port)
		return "", errors.New("服务可能没有注册")
	}

	source = host + ":" + port + "/debug/pprof/profile"
	return source, err
}

// GetHTTPServeHostPort 获取本 pprof 服务的 IP 和端口
func GetHTTPServeHostPort() string {
	return Config.Host + ":" + Config.Port
}
