package main

import (
	"path/filepath"
	"flag"
	"fmt"
	"os"
	"strconv"
	"github.com/magiconair/properties"
)

type configMap struct {
	port int
	root string
	virtualHosts map[string]string
	proxyContexts map[string]int
}

const (
	DEFAULT_PORT = 8080
	DEFAULT_ROOT = "root"
	DEFAULT_CONFIG = "./config.properties"
)

// command-line > properties > defaults
func getConfig() configMap {
	var port int
	flag.IntVar(&port, "port", 0, "'port' must be an int")

	var root string
	flag.StringVar(&root, "root", "", "'root' must be a valid path")

	var configPath string
	flag.StringVar(&configPath, "config", "", "'config' must be a valid path")

	flag.Parse()

	if configPath == "" {
		configPath = DEFAULT_CONFIG
	}
	configPath, _ = filepath.Abs(configPath)

	configProperties := properties.MustLoadFile(configPath, properties.UTF8)

	if port == 0 {
		port = configProperties.GetInt("port", DEFAULT_PORT)
	}

	if root == "" {
		root = configProperties.GetString("root", DEFAULT_ROOT)
	}
	root, _ = filepath.Abs(root)

	config := configMap{
		port: port,
		root: root,
		virtualHosts: make(map[string]string),
		proxyContexts: make(map[string]int),
	}

	for _, key := range configProperties.Keys() {
		if string(key[0]) == "/" {
			value := configProperties.GetString(key, "")
			if string(value[0]) == ":" {
				portNum, portErr := strconv.Atoi(value[1:])
				if portErr != nil {
					fmt.Printf("invalid port value for proxy-context in %v :'%v'\n", configPath, value)
					os.Exit(1)
				}
				
				config.proxyContexts[key] = portNum
			} else {
				config.virtualHosts[key] = value
			}
		}
	}

	return config
}