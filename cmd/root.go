package cmd

import (
	"crypto/tls"
	"flag"
	"fmt"
	"github.com/jinzhu/copier"
	"github.com/rs/xid"
	"github.com/tidwall/gjson"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	app "com.tester/utils"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

var (
	scenarios    []app.Scenario
	config       app.Config
	allScenarios []app.Scenario
	regex = regexp.MustCompile("{{(.*?)}}")
)

//GetConfigs func
func GetConfigs(configsFile *string) app.Config {
	log.Info("Loading test configurations ..:)")
	flag.Parse()
	if *configsFile == "" {
		flag.PrintDefaults()
		os.Exit(1)
	}
	if !strings.HasSuffix(*configsFile, "yaml") {
		log.Println("provide a configuration yaml file.")
		os.Exit(1)
	}
	abs, err := filepath.Abs(*configsFile)
	if err != nil {
		log.Fatalln("Error reading configs file: Cause: ",err)
	}

	testData, err := ioutil.ReadFile(abs)
	if err != nil {
		log.Fatalln("Error reading configs file: Cause: ",err)
	}

	err = yaml.Unmarshal(testData, &config)
	if err != nil {
		log.Fatalln(err)
	}
	return GetAccessToken(config)
}

//GetScenarios func
func GetScenarios(scenarioFolder *string) []app.Scenario {
	log.Info("Loading Test Scenarios ..:)")
	flag.Parse()
	if *scenarioFolder == "" {
		flag.PrintDefaults()
		os.Exit(1)
	}
	scenarioFolders, err := getWalking(*scenarioFolder)
	if err != nil {
		log.Fatal("Error opening folder ", err)
	}

	for _, file := range scenarioFolders {
		if strings.HasSuffix(file, "yaml") {
			abs, err := filepath.Abs(file)
			if err != nil {
				log.Fatalln("Error reading scenario file: Cause: ",err)
			}
			data, err := ioutil.ReadFile(abs)
			if err != nil {
				log.Fatalln("Error reading scenario file: Cause: ",err)
			}
			err = yaml.Unmarshal(data, &scenarios)
			if err != nil {
				log.Fatalln("Error reading scenario file: Cause: ",err)
			}
			for i, scenario := range scenarios {
				scenario.ID = getScenarioID(i)
				if scenario.Replicas > 0 {
					for j := 1; j < scenario.Replicas; j++ {
						scenario.ID = getScenarioID(j)
						innerScenario := app.Scenario{}
						copier.Copy(&innerScenario, scenario)
						allScenarios = append(allScenarios, innerScenario)
					}
				}
				allScenarios = append(allScenarios, scenario)
			}
		}
	}
	return allScenarios
}


func getScenarioID(id int) string{
	guid := xid.New()
	rep := guid.String()
	return fmt.Sprintf("SN-%d-%s",id,rep)
}

func getWalking(root string) ([]string, error) {
	subDirToSkip := "skip"
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("Error Walking the path %q: %v\n", path, err)
			return err
		}
		if info.IsDir() && info.Name() == subDirToSkip {
			fmt.Printf("skipping directory without errors: %+v \n", info.Name())
			return filepath.SkipDir
		}
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}


func GetAccessToken(config app.Config) app.Config{
	if config.InitFunc.Active {
		log.Println("generating access token")
		if config.InitFunc.Method != "" {
			config.InitFunc.Method = strings.ToUpper(config.InitFunc.Method)
		}
		req, err := http.NewRequest(config.InitFunc.Method, config.InitFunc.URL, nil)
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		client := &http.Client{}
		client.Transport = transport
		if err != nil {
			log.Error(err)
		}
		if len(config.InitFunc.Headers) != 0 {
			for k, v := range config.InitFunc.Headers {
				req.Header[k] = []string{v}
			}
		} else {
			req.Header["Content-Type"] = []string{"application/json"}
		}
		res, err := client.Do(req)
		defer res.Body.Close()
		body, err := ioutil.ReadAll(res.Body)
		tkn := gjson.Get(string(body), config.InitFunc.GetValue).String()
		accessToken := fmt.Sprintf("Bearer %s", tkn)
		config.Headers[config.InitFunc.TargetValue] = accessToken
		return config
	}
	return config
}
