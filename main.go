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
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"time"

	"pproflame/config"
	"pproflame/driver"

	"github.com/gin-gonic/gin"
)

func getServicePprof(c *gin.Context) {
	serviceName := c.Param("servicename") // 获取服务名称, 对应配置文件的 source(host, port)
	if len(serviceName) == 0 {
		log.Println("请指定需要采集的服务名称")
		return
	}
	log.Panicln("查询服务: ", serviceName)

	source, err := config.GetServiceSource(serviceName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取服务 pprof 接口错误: %v\n", err)
		os.Exit(2)
	}

	log.Println("请求源地址: ", c.ClientIP())
	log.Println("服务: ", serviceName, "的 pprof 地址是: ", source)

	driver.PProf(&driver.Options{}, source, config.GetHTTPServeHostPort())
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	router := gin.Default()

	err := config.LoadConfig()
	if err != nil {
		log.Panicln("读取配置文件失败: ", err)
		return
	}

	router.GET("/flamegraph/:servicename", getServicePprof)

	srv := &http.Server{
		Addr:    strconv.Itoa(config.Config.Port),
		Handler: router,
	}

	go func() {
		// service connections
		if err := srv.ListenAndServe(); err != nil {
			log.Printf("listen: %s\n", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server with
	// a timeout of 5 seconds.
	quit := make(chan os.Signal)
	signal.Notify(quit, os.Interrupt)
	<-quit
	log.Println("Shutdown Server ...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server Shutdown:", err)
	}
	log.Println("Server exiting")
}
