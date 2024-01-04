package main

import (
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"github.com/rs/zerolog"
    "github.com/rs/zerolog/log"
	"github.com/magiconair/properties"
)

type configMap struct {
	port          int
	root          string
	virtualHosts  map[string]string
	proxyContexts map[string]int
	logLevel 	  zerolog.Level
}

const (
	DEFAULT_PORT   = 8080
	DEFAULT_ROOT   = "root"
	DEFAULT_CONFIG = "./config.properties"
	DEFAULT_LOG_LEVEL = zerolog.InfoLevel
)

// command-line > properties > defaults
func getConfig() configMap {
	var port int
	flag.IntVar(&port, "port", 0, "'port' must be an int")

	var root string
	flag.StringVar(&root, "root", "", "'root' must be a valid path")

	var configPath string
	flag.StringVar(&configPath, "config", "", "'config' must be a valid path")

	var logLevel string
	flag.StringVar(&logLevel, "logLevel", "", "'logLevel' must be a valid string")

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
	
	if logLevel == "" {
		logLevel = configProperties.GetString("logLevel", DEFAULT_LOG_LEVEL.String())
	}
	zerologLevel, zErr := zerolog.ParseLevel(logLevel)
	if zErr != nil {
		log.Fatal().Err(zErr).Msgf("Invalid log level %v", logLevel)
	}

	config := configMap{
		port:          port,
		root:          root,
		virtualHosts:  make(map[string]string),
		proxyContexts: make(map[string]int),
		logLevel:      zerologLevel,
	}

	for _, key := range configProperties.Keys() {
		if string(key[0]) == "/" {
			value := configProperties.GetString(key, "")
			if string(value[0]) == ":" {
				portNum, portErr := strconv.Atoi(value[1:])
				if portErr != nil {
					log.Fatal().Msgf("invalid port value for proxy-context in %v: %v'", configPath, value)
				}

				config.proxyContexts[key] = portNum
			} else {
				config.virtualHosts[key] = value
			}
		}
	}

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	zerolog.SetGlobalLevel(config.logLevel)

	log.Info().Msgf("final configuration: %v", config)

	return config
}
