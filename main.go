// Copyright 2014 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// pprof is a tool for collection, manipulation and visualization
// of performance profiles.
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"pproflame/config"
	"pproflame/driver"
	internaldriver "pproflame/internal/driver"
	"strconv"
	"sync"

	"github.com/gin-gonic/gin"
)

// 全局 map 用于保存不同服务的 UI 对象.
// 如果要重新生成, 可以指定参数, 删除前确认 key 存在, 不需要加锁, 不会冲突
var mapUIObj sync.Map

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile | log.Ltime | log.LUTC)

	router := gin.Default()

	err := config.LoadConfig()
	if err != nil {
		log.Panicln("读取配置文件失败: ", err)
		return
	}

	router.GET("/", getPProfRoot)
	router.GET("/top", getPProfTop)
	router.GET("/disasm", getPProfDisasm)
	router.GET("/source", getPProfSource)
	router.GET("/peek", getPProfPeek)
	router.GET("/flamegraph", getPProfFlamegraph)

	router.Run(":" + config.Config.Port)
}

// getPProfRoot 渲染 ui.Root
func getPProfRoot(c *gin.Context) {
	serviceName := c.Query("servicename") // 获取服务名称, 对应配置文件的 source(host, port)

	if len(serviceName) == 0 {
		log.Println("请指定需要采集的服务名称")
		c.String(http.StatusBadRequest, "请指定需要采集的服务名称")
	}
	log.Println("查询服务: ", serviceName)

	seconds, _ := strconv.Atoi(c.Query("seconds"))

	if seconds == 0 {
		seconds = 30
	}
	log.Println("采样时间: ", seconds)

	reset, _ := strconv.Atoi(c.Query("reset")) // 1 表示重置采样, 0 表示不需要重置

	source, err := config.GetServiceSource(serviceName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取服务 pprof 接口错误: %v\n", err)
		return
	}

	log.Println("请求源地址: ", c.ClientIP())
	log.Println("服务名称: ", serviceName, "的 pprof 地址是: ", source)
	log.Println("是否重置采样: ", reset == 1)

	// 指定服务的 UI 对象已经存在, 直接给 top/disasm/dot/source/peek/flamegraph 复用, 否则重新采样拉取
	// Load returns the value stored in the map for a key, or nil if no
	// value is present.
	reSample := false
	if value, ok := mapUIObj.Load(serviceName); ok {
		webUI, valid := value.(*internaldriver.WebInterface)
		if valid {
			driver.SMMPProfRoot(webUI, c)
		} else {
			reSample = true
		}

	} else {
		reSample = true
	}

	// 重采样
	if reSample {
		driver.SMMCleanTempFiles()   // 清临时文件
		mapUIObj.Delete(serviceName) // 删旧 WebInterface 对象

		// NOTE: 服务不存在则重新采样
		ui, err := driver.SMMPProf(&driver.Options{}, source, 30)
		if err != nil {
			log.Println("采样失败: ", serviceName)
			return
		}

		mapUIObj.Store(serviceName, ui)
		driver.SMMPProfRoot(ui, c)
	}
}

// getPProfTop 渲染 ui.Top
func getPProfTop(c *gin.Context) {
	serviceName := c.Query("servicename") // 获取服务名称, 对应配置文件的 source(host, port)

	if len(serviceName) == 0 {
		log.Println("请指定需要采集的服务名称")
		c.String(400, "请指定需要采集的服务名称")
	}
	log.Println("查询服务: ", serviceName)

	source, err := config.GetServiceSource(serviceName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取服务 pprof 接口错误: %v\n", err)
		return
	}

	log.Println("请求源地址: ", c.ClientIP())
	log.Println("服务: ", serviceName, "的 pprof 地址是: ", source)

	// 指定服务的 UI 对象已经存在, 直接使用, 否则重新采样拉取
	// Load returns the value stored in the map for a key, or nil if no
	// value is present.
	reSample := false
	if value, ok := mapUIObj.Load(serviceName); ok {
		webUI, valid := value.(*internaldriver.WebInterface)
		if valid {
			driver.SMMPProfTop(webUI, c)
		} else {
			reSample = true
		}

	} else {
		reSample = true
	}

	// 重采样
	if reSample {
		driver.SMMCleanTempFiles()   // 清临时文件
		mapUIObj.Delete(serviceName) // 删旧 WebInterface 对象

		// NOTE: 服务不存在则重新采样
		ui, err := driver.SMMPProf(&driver.Options{}, source, 30)
		if err != nil {
			log.Println("采样失败: ", serviceName)
			return
		}

		mapUIObj.Store(serviceName, ui)
		driver.SMMPProfTop(ui, c)
	}
}

// getPProfDisasm 渲染 ui.disasm
func getPProfDisasm(c *gin.Context) {
	serviceName := c.Query("servicename") // 获取服务名称, 对应配置文件的 source(host, port)

	if len(serviceName) == 0 {
		log.Println("请指定需要采集的服务名称")
		c.String(400, "请指定需要采集的服务名称")
	}
	log.Println("查询服务: ", serviceName)

	source, err := config.GetServiceSource(serviceName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取服务 pprof 接口错误: %v\n", err)
		return
	}

	log.Println("请求源地址: ", c.ClientIP())
	log.Println("服务: ", serviceName, "的 pprof 地址是: ", source)

	// 指定服务的 UI 对象已经存在, 直接使用, 否则重新采样拉取
	// Load returns the value stored in the map for a key, or nil if no
	// value is present.
	reSample := false
	if value, ok := mapUIObj.Load(serviceName); ok {
		webUI, valid := value.(*internaldriver.WebInterface)
		if valid {
			driver.SMMPProfDisasm(webUI, c)
		} else {
			reSample = true
		}

	} else {
		reSample = true
	}

	// 重采样
	if reSample {
		driver.SMMCleanTempFiles()   // 清临时文件
		mapUIObj.Delete(serviceName) // 删旧 WebInterface 对象

		// NOTE: 服务不存在则重新采样
		ui, err := driver.SMMPProf(&driver.Options{}, source, 30)
		if err != nil {
			log.Println("采样失败: ", serviceName)
			return
		}

		mapUIObj.Store(serviceName, ui)
		driver.SMMPProfDisasm(ui, c)
	}
}

// getPProfSource 渲染 ui.Source
func getPProfSource(c *gin.Context) {
	serviceName := c.Query("servicename") // 获取服务名称, 对应配置文件的 source(host, port)

	if len(serviceName) == 0 {
		log.Println("请指定需要采集的服务名称")
		c.String(400, "请指定需要采集的服务名称")
	}
	log.Println("查询服务: ", serviceName)

	source, err := config.GetServiceSource(serviceName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取服务 pprof 接口错误: %v\n", err)
		return
	}

	log.Println("请求源地址: ", c.ClientIP())
	log.Println("服务: ", serviceName, "的 pprof 地址是: ", source)

	// 指定服务的 UI 对象已经存在, 直接使用, 否则重新采样拉取
	// Load returns the value stored in the map for a key, or nil if no
	// value is present.
	reSample := false
	if value, ok := mapUIObj.Load(serviceName); ok {
		webUI, valid := value.(*internaldriver.WebInterface)
		if valid {
			driver.SMMPProfSource(webUI, c)
		} else {
			reSample = true
		}

	} else {
		reSample = true
	}

	// 重采样
	if reSample {
		driver.SMMCleanTempFiles()   // 清临时文件
		mapUIObj.Delete(serviceName) // 删旧 WebInterface 对象

		// NOTE: 服务不存在则重新采样
		ui, err := driver.SMMPProf(&driver.Options{}, source, 30)
		if err != nil {
			log.Println("采样失败: ", serviceName)
			return
		}

		mapUIObj.Store(serviceName, ui)
		driver.SMMPProfSource(ui, c)
	}
}

// getPProfPeek 渲染 ui.Peek
func getPProfPeek(c *gin.Context) {
	serviceName := c.Query("servicename") // 获取服务名称, 对应配置文件的 source(host, port)

	if len(serviceName) == 0 {
		log.Println("请指定需要采集的服务名称")
		c.String(400, "请指定需要采集的服务名称")
	}
	log.Println("查询服务: ", serviceName)

	source, err := config.GetServiceSource(serviceName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取服务 pprof 接口错误: %v\n", err)
		return
	}

	log.Println("请求源地址: ", c.ClientIP())
	log.Println("服务: ", serviceName, "的 pprof 地址是: ", source)

	// 指定服务的 UI 对象已经存在, 直接使用, 否则重新采样拉取
	// Load returns the value stored in the map for a key, or nil if no
	// value is present.
	reSample := false
	if value, ok := mapUIObj.Load(serviceName); ok {
		webUI, valid := value.(*internaldriver.WebInterface)
		if valid {
			driver.SMMPProfPeek(webUI, c)
		} else {
			reSample = true
		}

	} else {
		reSample = true
	}

	// 重采样
	if reSample {
		driver.SMMCleanTempFiles()   // 清临时文件
		mapUIObj.Delete(serviceName) // 删旧 WebInterface 对象

		// NOTE: 服务不存在则重新采样
		ui, err := driver.SMMPProf(&driver.Options{}, source, 30)
		if err != nil {
			log.Println("采样失败: ", serviceName)
			return
		}

		mapUIObj.Store(serviceName, ui)
		driver.SMMPProfPeek(ui, c)
	}
}

// getPProfFlamegraph 渲染 ui.Flamegraph
func getPProfFlamegraph(c *gin.Context) {
	serviceName := c.Query("servicename") // 获取服务名称, 对应配置文件的 source(host, port)

	if len(serviceName) == 0 {
		log.Println("请指定需要采集的服务名称")
		c.String(400, "请指定需要采集的服务名称")
	}
	log.Println("查询服务: ", serviceName)

	source, err := config.GetServiceSource(serviceName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取服务 pprof 接口错误: %v\n", err)
		return
	}

	log.Println("请求源地址: ", c.ClientIP())
	log.Println("服务: ", serviceName, "的 pprof 地址是: ", source)

	// 指定服务的 UI 对象已经存在, 直接使用, 否则重新采样拉取
	// Load returns the value stored in the map for a key, or nil if no
	// value is present.
	reSample := false
	if value, ok := mapUIObj.Load(serviceName); ok {
		webUI, valid := value.(*internaldriver.WebInterface)
		if valid {
			driver.SMMPProfFlamegraph(webUI, c)
		} else {
			reSample = true
		}

	} else {
		reSample = true
	}

	// 重采样
	if reSample {
		driver.SMMCleanTempFiles()   // 清临时文件
		mapUIObj.Delete(serviceName) // 删旧 WebInterface 对象

		// NOTE: 服务不存在则重新采样
		ui, err := driver.SMMPProf(&driver.Options{}, source, 30)
		if err != nil {
			log.Println("采样失败: ", serviceName)
			return
		}

		mapUIObj.Store(serviceName, ui)
		driver.SMMPProfFlamegraph(ui, c)
	}
}
